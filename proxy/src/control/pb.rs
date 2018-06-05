#![allow(dead_code)]
#![cfg_attr(feature = "cargo-clippy", allow(clippy))]

use std::error::Error;
use std::fmt;
use std::sync::Arc;

use conduit_proxy_controller_grpc::*;
use convert::*;
use ctx;
use telemetry::{event, Event};

#[derive(Debug, Clone)]
pub struct UnknownEvent;

impl fmt::Display for UnknownEvent {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "unknown tap event")
    }
}

impl Error for UnknownEvent {
    #[inline]
    fn description(&self) -> &str {
        "unknown tap event"
    }
}

impl event::StreamResponseEnd {
    fn to_tap_event(&self, ctx: &Arc<ctx::http::Request>) -> common::TapEvent {
        use ::conduit_proxy_controller_grpc::common::{tap_event, Eos};

        let eos = self.grpc_status
            .map(Eos::from_grpc_status)
            ;

        let end = tap_event::http::ResponseEnd {
            id: Some(tap_event::http::StreamId {
                base: 0, // TODO FIXME
                stream: ctx.id.into(),
            }),
            since_request_init: Some(pb_elapsed(self.request_open_at, self.response_end_at)),
            since_response_init: Some(pb_elapsed(self.response_open_at, self.response_end_at)),
            response_bytes: self.bytes_sent,
            eos,
        };

        let destination_meta = ctx.dst_labels()
            .map(|ref d| tap_event::EndpointMeta {
                labels: d.as_map().clone(),
            });

        common::TapEvent {
            source: Some((&ctx.server.remote).into()),
            destination: Some((&ctx.client.remote).into()),
            destination_meta,
            event: Some(tap_event::Event::Http(tap_event::Http {
                event: Some(tap_event::http::Event::ResponseEnd(end)),
            })),
        }
    }
}

impl event::StreamResponseFail {
    fn to_tap_event(&self, ctx: &Arc<ctx::http::Request>) -> common::TapEvent {
        use self::common::tap_event;

        let end = tap_event::http::ResponseEnd {
            id: Some(tap_event::http::StreamId {
                base: 0, // TODO FIXME
                stream: ctx.id.into(),
            }),
            since_request_init: Some(pb_elapsed(self.request_open_at, self.response_fail_at)),
            since_response_init: Some(pb_elapsed(self.response_open_at, self.response_fail_at)),
            response_bytes: self.bytes_sent,
            eos: Some(self.error.into()),
        };

        let destination_meta = ctx.dst_labels()
            .map(|ref d| tap_event::EndpointMeta {
                labels: d.as_map().clone(),
            });

        common::TapEvent {
            source: Some((&ctx.server.remote).into()),
            destination: Some((&ctx.client.remote).into()),
            destination_meta,
            event: Some(tap_event::Event::Http(tap_event::Http {
                event: Some(tap_event::http::Event::ResponseEnd(end)),
            })),
        }
    }
}

impl event::StreamRequestFail {
    fn to_tap_event(&self, ctx: &Arc<ctx::http::Request>) -> common::TapEvent {
        use self::common::tap_event;

        let end = tap_event::http::ResponseEnd {
            id: Some(tap_event::http::StreamId {
                base: 0, // TODO FIXME
                stream: ctx.id.into(),
            }),
            since_request_init: Some(pb_elapsed(self.request_open_at, self.request_fail_at)),
            since_response_init: None,
            response_bytes: 0,
            eos: Some(self.error.into()),
        };

        let destination_meta = ctx.dst_labels()
            .map(|ref d| tap_event::EndpointMeta {
                labels: d.as_map().clone(),
            });

        common::TapEvent {
            source: Some((&ctx.server.remote).into()),
            destination: Some((&ctx.client.remote).into()),
            destination_meta,
            event: Some(tap_event::Event::Http(tap_event::Http {
                event: Some(tap_event::http::Event::ResponseEnd(end)),
            })),
        }
    }
}

impl<'a> TryFrom<&'a Event> for common::TapEvent {
    type Err = UnknownEvent;
    fn try_from(ev: &'a Event) -> Result<Self, Self::Err> {
        use self::common::tap_event;

        let tap_ev = match *ev {
            Event::StreamRequestOpen(ref ctx) => {
                let init = tap_event::http::RequestInit {
                    id: Some(tap_event::http::StreamId {
                        base: 0,
                        // TODO FIXME
                        stream: ctx.id.into(),
                    }),
                    method: Some((&ctx.method).into()),
                    scheme: ctx.uri.scheme_part().map(common::Scheme::from),
                    authority: ctx.uri
                        .authority_part()
                        .map(|a| a.as_str())
                        .unwrap_or_default()
                        .into(),
                    path: ctx.uri.path().into(),
                };

                let destination_meta = ctx.dst_labels()
                    .map(|ref d| tap_event::EndpointMeta {
                        labels: d.as_map().clone(),
                    });

                common::TapEvent {
                    source: Some((&ctx.server.remote).into()),
                    destination: Some((&ctx.client.remote).into()),
                    destination_meta,
                    event: Some(tap_event::Event::Http(tap_event::Http {
                        event: Some(tap_event::http::Event::RequestInit(init)),
                    })),
                }
            }

            Event::StreamResponseOpen(ref ctx, ref rsp) => {
                let init = tap_event::http::ResponseInit {
                    id: Some(tap_event::http::StreamId {
                        base: 0,
                        // TODO FIXME
                        stream: ctx.request.id.into(),
                    }),
                    since_request_init: Some(pb_elapsed(rsp.request_open_at, rsp.response_open_at)),
                    http_status: u32::from(ctx.status.as_u16()),
                };

                let destination_meta = ctx.dst_labels()
                    .map(|ref d| tap_event::EndpointMeta {
                        labels: d.as_map().clone(),
                    });

                common::TapEvent {
                    source: Some((&ctx.request.server.remote).into()),
                    destination: Some((&ctx.request.client.remote).into()),
                    destination_meta,
                    event: Some(tap_event::Event::Http(tap_event::Http {
                        event: Some(tap_event::http::Event::ResponseInit(init)),
                    })),
                }
            }

            Event::StreamRequestFail(ref ctx, ref fail) => {
                fail.to_tap_event(&ctx)
            }

            Event::StreamResponseEnd(ref ctx, ref end) => {
                end.to_tap_event(&ctx.request)
            }

            Event::StreamResponseFail(ref ctx, ref fail) => {
                fail.to_tap_event(&ctx.request)
            }

            _ => return Err(UnknownEvent),
        };

        Ok(tap_ev)
    }
}
