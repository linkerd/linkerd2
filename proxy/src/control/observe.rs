use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicUsize, Ordering};

use futures::{future, Poll, Stream};
use futures_mpsc_lossy;
use http::HeaderMap;
use indexmap::{Equivalent, IndexSet};
use tower_grpc::{self as grpc, Response};

use conduit_proxy_controller_grpc::common::TapEvent;
use conduit_proxy_controller_grpc::tap::{server, ObserveRequest};
use convert::*;
use ctx;
use telemetry::Event;
use telemetry::tap::{Tap, Taps};
use std::hash::{Hash, Hasher};

#[derive(Clone, Debug)]
pub struct Observe {
    next_id: Arc<AtomicUsize>,
    taps: Arc<Mutex<Taps>>,
    tap_capacity: usize,
}

pub struct TapEvents {
    rx: futures_mpsc_lossy::Receiver<Event>,
    remaining: usize,
    current: IndexSet<RequestById>,
    tap_id: usize,
    taps: Arc<Mutex<Taps>>,
}

// `IndexSet<RequestById>` is equivalent to `IndexMap<RequestId, Request>` but
// avoids storing the `RequestID` twice.
struct RequestById(Arc<ctx::http::Request>);

impl Observe {
    pub fn new(tap_capacity: usize) -> (Arc<Mutex<Taps>>, Observe) {
        let taps = Arc::new(Mutex::new(Taps::default()));

        let observe = Observe {
            next_id: Arc::new(AtomicUsize::new(0)),
            tap_capacity,
            taps: taps.clone(),
        };

        (taps, observe)
    }
}

impl server::Tap for Observe {
    type ObserveStream = TapEvents;
    type ObserveFuture = future::FutureResult<Response<Self::ObserveStream>, grpc::Error>;

    fn observe(&mut self, req: grpc::Request<ObserveRequest>) -> Self::ObserveFuture {
        if self.next_id.load(Ordering::Acquire) == ::std::usize::MAX {
            return future::err(grpc::Error::Grpc(
                grpc::Status::INTERNAL,
                HeaderMap::new(),
            ));
        }

        let req = req.into_inner();
        let (tap, rx) = match req.match_
            .and_then(|m| Tap::new(&m, self.tap_capacity).ok())
        {
            Some(m) => m,
            None => {
                return future::err(grpc::Error::Grpc(
                    grpc::Status::INVALID_ARGUMENT,
                    HeaderMap::new(),
                ));
            }
        };

        let tap_id = match self.taps.lock() {
            Ok(mut taps) => {
                let tap_id = self.next_id.fetch_add(1, Ordering::AcqRel);
                let _ = (*taps).insert(tap_id, tap);
                tap_id
            }
            Err(_) => {
                return future::err(grpc::Error::Grpc(
                    grpc::Status::INTERNAL,
                    HeaderMap::new(),
                ));
            }
        };

        let events = TapEvents {
            rx,
            tap_id,
            current: IndexSet::default(),
            remaining: req.limit as usize,
            taps: self.taps.clone(),
        };

        future::ok(Response::new(events))
    }
}

impl Stream for TapEvents {
    type Item = TapEvent;
    type Error = grpc::Error;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        loop {
            if self.remaining == 0 && self.current.is_empty() {
                return Ok(None.into());
            }

            let poll: Poll<Option<Event>, Self::Error> =
                self.rx.poll().or_else(|_| Ok(None.into()));

            match try_ready!(poll) {
                Some(ev) => {
                    match ev {
                        Event::StreamRequestOpen(ref req) => {
                            if self.remaining == 0 {
                                continue;
                            }
                            self.remaining -= 1;
                            let _ = self.current.insert(RequestById(req.clone()));
                        }
                        Event::StreamRequestFail(ref req, _) => {
                            if !self.current.remove(&req.id) {
                                continue;
                            }
                        }
                        Event::StreamResponseOpen(ref rsp, _) => {
                            if !self.current.contains(&rsp.request.id) {
                                continue;
                            }
                        }
                        Event::StreamResponseFail(ref rsp, _) |
                        Event::StreamResponseEnd(ref rsp, _) => {
                            if !self.current.remove(&rsp.request.id) {
                                continue;
                            }
                        }
                        _ => continue,
                    }

                    if let Ok(te) = TapEvent::try_from(&ev) {
                        // TODO Do limit checks here.
                        return Ok(Some(te).into());
                    }
                }
                None => {
                    return Ok(None.into());
                }
            }
        }
    }
}

impl Drop for TapEvents {
    fn drop(&mut self) {
        if let Ok(mut taps) = self.taps.lock() {
            let _ = (*taps).remove(self.tap_id);
        }
    }
}

impl Eq for RequestById {}

impl PartialEq for RequestById {
    fn eq(&self, other: &RequestById) -> bool {
        self.0.id.eq(&other.0.id)
    }
}

impl Hash for RequestById {
    fn hash<H: Hasher>(&self, state: &mut H) {
        self.0.id.hash(state)
    }
}

impl Equivalent<RequestById> for ctx::http::RequestId {
    /// Compare self to `key` and return `true` if they are equal.
    fn equivalent(&self, key: &RequestById) -> bool {
        *self == key.0.id
    }
}
