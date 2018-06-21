use futures::{Async, Poll, Stream};
use futures_watch::Watch;
use tower_service::Service;

pub trait Rebind<T> {
    type Service: Service;
    fn rebind(&mut self, t: &T) -> Self::Service;
}

/// A Service that updates itself as a Watch updates.
#[derive(Debug)]
pub struct WatchService<T, R: Rebind<T>> {
    watch: Watch<T>,
    rebind: R,
    inner: R::Service,
}

impl<T, R: Rebind<T>> WatchService<T, R> {
    pub fn new(watch: Watch<T>, mut rebind: R) -> WatchService<T, R> {
        let inner = rebind.rebind(&*watch.borrow());
        WatchService { watch, rebind, inner }
    }
}

impl<T, R: Rebind<T>> Service for WatchService<T, R> {
    type Request = <R::Service as Service>::Request;
    type Response = <R::Service as Service>::Response;
    type Error = <R::Service as Service>::Error;
    type Future = <R::Service as Service>::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        // Check to see if the watch has been updated and, if so, rebind the service.
        //
        // `watch.poll()` can't actually fail; so errors are not considered.
        while let Ok(Async::Ready(Some(()))) = self.watch.poll() {
            self.inner = self.rebind.rebind(&*self.watch.borrow());
        }

        self.inner.poll_ready()
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        self.inner.call(req)
    }
}

impl<T, S, F> Rebind<T> for F
where
    S: Service,
    for<'t> F: FnMut(&'t T) -> S,
{
    type Service = S;
    fn rebind(&mut self, t: &T) -> S {
        (self)(t)
    }
}

#[cfg(test)]
mod tests {
    use futures::future;
    use std::time::Duration;
    use task::test_util::BlockOnFor;
    use tokio::runtime::current_thread::Runtime;
    use super::*;

    const TIMEOUT: Duration = Duration::from_secs(60);

    #[test]
    fn rebind() {
        struct Svc(usize);
        impl Service for Svc {
            type Request = ();
            type Response = usize;
            type Error = ();
            type Future = future::FutureResult<usize, ()>;
            fn poll_ready(&mut self) -> Poll<(), Self::Error> {
                Ok(().into())
            }
            fn call(&mut self, _: ()) -> Self::Future {
                future::ok(self.0)
            }
        }

        let mut rt = Runtime::new().unwrap();
        macro_rules! assert_ready {
            ($svc:expr) => {
                rt.block_on_for(TIMEOUT, future::poll_fn(|| $svc.poll_ready()))
                    .expect("ready")
            };
        }
        macro_rules! call {
            ($svc:expr) => {
                rt.block_on_for(TIMEOUT, $svc.call(()))
                    .expect("call")
            };
        }

        let (watch, mut store) = Watch::new(1);
        let mut svc = WatchService::new(watch, |n: &usize| Svc(*n));

        assert_ready!(svc);
        assert_eq!(call!(svc), 1);

        assert_ready!(svc);
        assert_eq!(call!(svc), 1);

        store.store(2).expect("store");
        assert_ready!(svc);
        assert_eq!(call!(svc), 2);

        store.store(3).expect("store");
        store.store(4).expect("store");
        assert_ready!(svc);
        assert_eq!(call!(svc), 4);

        drop(store);
        assert_ready!(svc);
        assert_eq!(call!(svc), 4);
    }
}
