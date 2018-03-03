extern crate futures;
extern crate indexmap;
extern crate tower;

use futures::{Future, Poll};
use indexmap::IndexMap;
use tower::Service;

use std::convert::AsRef;
use std::hash::Hash;
use std::mem;
use std::sync::{Arc, Mutex};

/// Route requests based on the request authority
pub struct Router<T>
where T: Recognize,
{
    inner: Arc<Mutex<Inner<T>>>,
}

/// Route a request based on an authority
pub trait Recognize {
    /// Requests handled by the discovered services
    type Request;

    /// Responses given by the discovered services
    type Response;

    /// Errors produced by the discovered services
    type Error;

    /// Key
    type Key: Clone + Eq + Hash;

    /// Error produced by failed routing
    type RouteError;

    /// The discovered `Service` instance.
    type Service: Service<Request = Self::Request,
                         Response = Self::Response,
                            Error = Self::Error>;

    /// Obtains a Key for a request.
    fn recognize(&self, req: &Self::Request) -> Option<Uses<Self::Key>>;

    /// Return a `Service` to handle requests from the provided authority.
    ///
    /// The returned service must always be in the ready state (i.e.
    /// `poll_ready` must always return `Ready` or `Err`).
    fn bind_service(&mut self, key: &Self::Key) -> Result<Self::Service, Self::RouteError>;
}

pub struct Single<S>(Option<S>);

/// Whether or not the service to a given key may be cached.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Uses<T> {
    Reusable(T),
    SingleUse(T),
}

#[derive(Debug)]
pub enum Error<T, U> {
    Inner(T),
    Route(U),
    NotRecognized,
}

pub struct ResponseFuture<T>
where T: Recognize,
{
    state: State<T>,
}

struct Inner<T>
where T: Recognize,
{
    routes: IndexMap<T::Key, T::Service>,
    recognize: T,
}

enum State<T>
where T: Recognize,
{
    Inner(<T::Service as Service>::Future),
    RouteError(T::RouteError),
    NotRecognized,
    Invalid,
}

// ===== impl Router =====

impl<T> Router<T>
where T: Recognize
{
    pub fn new(recognize: T) -> Self {
        Router {
            inner: Arc::new(Mutex::new(Inner {
                routes: Default::default(),
                recognize,
            })),
        }
    }
}

impl<T> Service for Router<T>
where T: Recognize,
{
    type Request = T::Request;
    type Response = T::Response;
    type Error = Error<T::Error, T::RouteError>;
    type Future = ResponseFuture<T>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let mut inner = self.inner.lock().unwrap();
        let inner = &mut *inner;

        // This insanity is to make the borrow checker happy...

        // These vars will be used to insert a new service in the cache.
        let new_key;
        let mut new_service;

        // This loop is used to create a borrow checker scope as well as being
        // able to call `break` to jump out of it.
        loop {
            let service;

            if let Some(key) = inner.recognize.recognize(&request) {
                // Is the bound service for that key reusable?
                let cached = if let Uses::Reusable(ref key) = key {
                    // The key is reusable --- look in the cache.
                    inner.routes.get_mut(key)
                } else {
                    // If the key is not reuseable, then the cached service is
                    // always None.
                    None
                };
                if let Some(s) = cached {
                    // The service for the authority is already cached
                    service = s;
                } else {
                    // The authority does not match an existing route, try to
                    // recognize it.
                    match inner.recognize.bind_service(key.as_ref()) {
                        Ok(s) => {
                            // A new service has been matched. Set the outer
                            // variables and jump out o the loop
                            new_key = key.clone();
                            new_service = s;
                            break;
                        }
                        Err(e) => {
                            // Route recognition failed
                            return ResponseFuture { state: State::RouteError(e) };
                        }
                    }
                }
            } else {
                // The request has no authority
                return ResponseFuture { state: State::NotRecognized };
            }

            // Route to the cached service.
            let response = service.call(request);
            return ResponseFuture { state: State::Inner(response) };
        }

        // First, route the request to the new service
        let response = new_service.call(request);

        // Now, cache the new service'
        if let Uses::Reusable(new_key) = new_key {
            inner.routes.insert(new_key, new_service);
        }

        // And finally, return the response
        ResponseFuture { state: State::Inner(response) }
    }
}

impl<T> Clone for Router<T>
where T: Recognize,
{
    fn clone(&self) -> Self {
        Router { inner: self.inner.clone() }
    }
}

// ===== impl Recognize =====

// ===== impl Single =====

impl<S: Service> Single<S> {
    pub fn new(svc: S) -> Self {
        Single(Some(svc))
    }
}

impl<S: Service> Recognize for Single<S> {
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type Key = ();
    type RouteError = ();
    type Service = S;

    fn recognize(&self, _: &Self::Request) -> Option<Uses<Self::Key>> {
        Some(Uses::Reusable(()))
    }

    fn bind_service(&mut self, _: &Self::Key) -> Result<S, Self::RouteError> {
        Ok(self.0.take().expect("static route bound twice"))
    }
}

// ===== impl ResponseFuture =====

impl<T> Future for ResponseFuture<T>
where T: Recognize,
{
    type Item = T::Response;
    type Error = Error<T::Error, T::RouteError>;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        use self::State::*;

        match self.state {
            Inner(ref mut fut) => fut.poll().map_err(Error::Inner),
            RouteError(..) => {
                match mem::replace(&mut self.state, Invalid) {
                    RouteError(e) => Err(Error::Route(e)),
                    _ => unreachable!(),
                }
            }
            NotRecognized => Err(Error::NotRecognized),
            Invalid => panic!(),
        }
    }
}

// ===== impl Uses=====

impl<T> AsRef<T> for Uses<T> {
    fn as_ref(&self) -> &T {
        match *self {
            Uses::Reusable(ref key) => key,
            Uses::SingleUse(ref key) => key,
        }
    }
}
