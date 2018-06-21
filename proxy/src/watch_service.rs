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
        if let Ok(Async::Ready(Some(()))) = self.watch.poll() {
            self.inner = self.rebind.rebind(&*self.watch.borrow());
        }

        self.inner.poll_ready()
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        self.inner.call(req)
    }
}

impl<T, S> Rebind<T> for FnMut(&T) -> S where S: Service
{
    type Service = S;
    fn rebind(&mut self, t: &T) -> S {
        (self)(t)
    }
}
