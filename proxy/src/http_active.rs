use std::marker::PhantomData;
use std::sync::{Arc, atomic::{AtomicUsize, Ordering}};
use futures::{Future, Poll};
use http;
use tower_service::Service;

use conduit_proxy_router::Retain;

/// Keeps an account of how many HTTP requests are active
pub struct HttpActive<S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    inner: S,
    gauge: Gauge,
}

pub struct Respond<F, B>
where
    F: Future<Item = http::Response<B>>,
{
    inner: F,
    gauge: Gauge,
}

pub struct RetainActive<K, S, A, B> {
    _p: PhantomData<(K, S, A, B)>,
}

/// Counts the number of active messages to determine gaugeness.
#[derive(Debug, Default, Clone)]
struct Gauge(Arc<AtomicUsize>);

/// A handle that decrements the number of active messages on drop.
#[derive(Debug)]
struct Active(Option<Arc<AtomicUsize>>);

// ===== impl RetainActive =====

impl<K, S, A, B> Default for RetainActive<K, S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    fn default() -> Self {
        Self { _p :PhantomData }
    }
}

impl<K, S, A, B> Retain<K, HttpActive<S, A, B>> for RetainActive<K, S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    /// Retains active endpoints
    fn retain(&self, _: &K, svc: &mut HttpActive<S, A, B>) -> bool {
        svc.gauge.is_active()
    }
}

// ===== impl Gauge =====

impl Gauge {
    fn is_active(&self) -> bool {
        self.0.load(Ordering::Acquire) > 0
    }

    pub fn active(&mut self) -> Active {
        self.0.fetch_add(1, Ordering::AcqRel);
        Active(Some(self.0.clone()))
    }
}

// ===== impl Active =====

impl Drop for Active {
    fn drop(&mut self) {
        if let Some(active) = self.0.take() {
            active.fetch_sub(1, Ordering::AcqRel);
        }
    }
}

// ===== impl HttpActive =====

impl<S, A, B> From<S> for HttpActive<S, A, B>
where
    S: Service<Request = http::Request<A>, Response = http::Response<B>>,
{
    fn from(inner: S) -> Self {
        Self {
            inner,
            gauge: Gauge::default(),
        }
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
        req.extensions_mut().insert(self.gauge.active());
        Respond {
            inner: self.inner.call(req),
            gauge: self.gauge.clone(),
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
        rsp.extensions_mut().insert(self.gauge.active());
        Ok(rsp.into())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn gauge_activity() {
        let mut gauge = Gauge::default();
        assert!(!gauge.is_active());

        let act0 = gauge.active();
        assert!(gauge.is_active());

        let act1 = gauge.active();
        assert!(gauge.is_active());

        drop(act0);
        assert!(gauge.is_active());

        drop(act1);
        assert!(!gauge.is_active());
    }
}
