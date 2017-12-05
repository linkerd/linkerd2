use std::sync::{Arc, Mutex};

use futures::{future, Stream, Poll};
use futures_mpsc_lossy;
use ordermap::OrderMap;
use tower_grpc::{self, Request, Response};
use tower_grpc::codegen::server::grpc::ServerStreamingService;

use control::pb::common::TapEvent;
use control::pb::proxy::tap::{ObserveRequest};
use ctx;
use telemetry::Event;
use telemetry::tap::{Tap, Taps};
use convert::*;

#[derive(Clone, Debug)]
pub struct Observe {
    next_id: usize,
    taps: Arc<Mutex<Taps>>,
    tap_capacity: usize,
}

pub struct TapEvents {
    rx: futures_mpsc_lossy::Receiver<Event>,
    remaining: usize,
    current: OrderMap<Arc<ctx::http::Request>, ()>,
    tap_id: usize,
    taps: Arc<Mutex<Taps>>,
}

impl Observe {
    pub fn new(tap_capacity: usize) -> (Arc<Mutex<Taps>>, Observe) {
        let taps = Arc::new(Mutex::new(Taps::default()));

        let observe = Observe {
            next_id: 0,
            tap_capacity,
            taps: taps.clone(),
        };

        (taps, observe)
    }
}

impl ServerStreamingService for Observe {
    type Request = ObserveRequest;
    type Response = TapEvent;
    type ResponseStream = TapEvents;
    type Future = future::FutureResult<Response<Self::ResponseStream>, tower_grpc::Error>;

    fn poll_ready(&mut self) -> Poll<(), tower_grpc::Error> {
        Ok(().into())
    }

    fn call(&mut self, req: Request<ObserveRequest>) -> Self::Future {
        if self.next_id == ::std::usize::MAX {
            return future::err(tower_grpc::Error::Grpc(tower_grpc::Status::INTERNAL));
        }

        let (_, req) = req.into_http().into_parts();
        let (tap, rx) = match req.match_.and_then(|m| Tap::new(&m, self.tap_capacity).ok()) {
            Some(m) => m,
            None => {
                return future::err(tower_grpc::Error::Grpc(tower_grpc::Status::INVALID_ARGUMENT));
            }
        };

        let tap_id = match self.taps.lock() {
            Ok(mut taps) => {
                let tap_id = self.next_id;
                self.next_id += 1;
                let _ = (*taps).insert(tap_id, tap);
                tap_id
            }
            Err(_) => {
                return future::err(tower_grpc::Error::Grpc(tower_grpc::Status::INTERNAL));
            }
        };

        let events = TapEvents {
            rx,
            tap_id,
            current: OrderMap::default(),
            remaining: req.limit as usize,
            taps: self.taps.clone(),
        };
        future::ok(Response::new(events))
    }
}

impl Stream for TapEvents {
    type Item = TapEvent;
    type Error = tower_grpc::Error;

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
                            let _ = self.current.insert(req.clone(), ());
                        }
                        Event::StreamRequestFail(ref req, _) => {
                            if self.current.remove(req).is_none() {
                                continue;
                            }
                        }
                        Event::StreamResponseOpen(ref rsp, _) => {
                            if !self.current.contains_key(&rsp.request) {
                                continue;
                            }
                        }
                        Event::StreamResponseFail(ref rsp, _) |
                        Event::StreamResponseEnd(ref rsp, _) => {
                            if self.current.remove(&rsp.request).is_none() {
                                continue;
                            }
                        }
                        _ => continue,
                    }

                    if let Ok(te) = (&ev).try_into() {
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
