use std::marker::PhantomData;
use std::sync::{Arc, atomic::{AtomicUsize, Ordering}};
use futures::{Future, Poll};
use http;
use tower_service::Service;

use conduit_proxy_router::Retain;

/// Keeps account of how many HTTP requests are active.
///
/// This service wraps any HTTP service and adds an extension to both requests and
/// responses so that, as long as the message is held in memory, the service is considered
/// active. This is intended to count long-lived streams in ways that, for instance,
/// `InFlightLimit` cannot.
///
/// ### TODO
/// - audit inner services to ensure that messages are not dropped before bodies.
#[derive(Debug)]
pub struct HttpActive<S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    /// An underlying HTTP service.
    inner: S,
    meter: Meter,
}

/// `HttpActive`'s response future.
///
/// Ensures that the response is tracked.
#[derive(Debug)]
pub struct Respond<F, B>
where
    F: Future<Item = http::Response<B>>,
{
    inner: F,
    meter: Meter,
}

/// An implementation of `Retain` for `HttpActive` values.
#[derive(Debug)]
pub struct RetainHttpActive<K, S, A, B> {
    _p: PhantomData<(K, S, A, B)>,
}

/// Counts the number of active messages to determine active-ness.
#[derive(Clone, Debug, Default)]
struct Meter(Arc<AtomicUsize>);

/// A handle that decrements the number of active messages on drop.
#[derive(Debug)]
struct Active(Option<Arc<AtomicUsize>>);

// ===== impl RetainHttpActive =====

impl<K, S, A, B> Default for RetainHttpActive<K, S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    fn default() -> Self {
        Self { _p :PhantomData }
    }
}

impl<K, S, A, B> Retain<K, HttpActive<S, A, B>> for RetainHttpActive<K, S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    /// Returns true iff `svc` has active requests.
    fn retain(&self, _: &K, svc: &mut HttpActive<S, A, B>) -> bool {
        svc.meter.count() > 0
    }
}

// ===== impl Meter =====

impl Meter {
    fn count(&self) -> usize {
        self.0.load(Ordering::Acquire)
    }

    pub fn active(&mut self) -> Active {
        self.0.fetch_add(1, Ordering::AcqRel);
        Active(Some(self.0.clone()))
    }
}

// ===== impl Active =====

impl Drop for Active {
    fn drop(&mut self) {
        if let Some(meter) = self.0.take() {
            meter.fetch_sub(1, Ordering::AcqRel);
        }
    }
}

// ===== impl HttpActive =====

impl<S, A, B> HttpActive<S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    pub fn new(inner: S) -> Self {
        let meter = Meter::default();
        Self { inner, meter }
    }
}

impl<S, A, B> Service for HttpActive<S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type Future = Respond<S::Future, B>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
    }

    fn call(&mut self, mut req: Self::Request) -> Self::Future {
        req.extensions_mut().insert(self.meter.active());
        Respond {
            inner: self.inner.call(req),
            meter: self.meter.clone(),
        }
    }
}

// ===== impl Respond =====

impl<F, B> Future for Respond<F, B>
where
    F: Future<Item = http::Response<B>>,
{
    type Item = http::Response<B>;
    type Error = F::Error;

    fn poll(&mut self) -> Poll<http::Response<B>, Self::Error> {
        let mut rsp = try_ready!(self.inner.poll());
        rsp.extensions_mut().insert(self.meter.active());
        Ok(rsp.into())
    }
}

#[cfg(test)]
mod tests {
    use futures::{future, Poll};
    use std::{cell::RefCell, collections::VecDeque, rc::Rc};
    use tower_service::Service;
    use super::*;

    #[test]
    fn meter_activity() {
        let mut meter = Meter::default();
        assert_eq!(meter.count(), 0);

        let act0 = meter.active();
        assert_eq!(meter.count(), 1);

        let act1 = meter.active();
        assert_eq!(meter.count(), 2);

        drop(act0);
        assert_eq!(meter.count(), 1);

        drop(act1);
        assert_eq!(meter.count(), 0);
    }

    #[test]
    fn http_active() {
        let svc = Svc::default();
        let reqs = svc.0.clone();

        let mut active = HttpActive::new(svc);
        assert_eq!(active.meter.count(), 0);

        let rsp0 = active.call_ok(Request::new(()));
        assert_eq!(active.meter.count(), 2);

        drop((*reqs.borrow_mut()).pop_front());
        assert_eq!(active.meter.count(), 1);

        let rsp1 = active.call_ok(Request::new(()));
        assert_eq!(active.meter.count(), 3);

        drop(rsp1);
        assert_eq!(active.meter.count(), 2);

        drop(rsp0);
        assert_eq!(active.meter.count(), 1);

        drop((*reqs.borrow_mut()).pop_front());
        assert_eq!(active.meter.count(), 0);
    }

    #[test]
    fn retain_http_active() {
        let svc = Svc::default();
        let reqs = svc.0.clone();

        let mut active = HttpActive::new(svc);
        let retain = RetainHttpActive::default();
        assert_eq!(retain.retain(&(), &mut active), false);

        let rsp0 = active.call_ok(Request::new(()));
        assert_eq!(retain.retain(&(), &mut active), true);

        drop((*reqs.borrow_mut()).pop_front());
        assert_eq!(retain.retain(&(), &mut active), true);

        drop(rsp0);
        assert_eq!(retain.retain(&(), &mut active), false);
    }

    impl HttpActive<Svc, (), ()> {
        fn call_ok(&mut self, req: Request) -> Response {
            self.call(req).wait().expect("should serve")
        }
    }

    type Request = http::Request<()>;
    type Response = http::Response<()>;

    #[derive(Debug, Default)]
    struct Svc(Rc<RefCell<VecDeque<Request>>>);

    impl Service for Svc {
        type Request = http::Request<()>;
        type Response = http::Response<()>;
        type Error = ();
        type Future = future::FutureResult<Self::Response, Self::Error>;

        fn poll_ready(&mut self) -> Poll<(), ()> {
            Ok(().into())
        }

        fn call(&mut self, req: Self::Request) -> Self::Future {
            (*self.0.borrow_mut()).push_back(req);
            future::ok(http::Response::new(()))
        }
    }

}
