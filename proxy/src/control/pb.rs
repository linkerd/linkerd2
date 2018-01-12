#![allow(dead_code)]
#![cfg_attr(feature = "cargo-clippy", allow(clippy))]

use std::error::Error;
use std::{fmt, hash};
use std::sync::Arc;

use http;

use convert::*;
use ctx;
use telemetry::Event;

// re-export proxy here since we dont care about the other dirs
pub use self::proxy::*;

pub mod proxy {
    // this is the struct expected by protoc, so make imports happy
    pub mod common {
        include!(concat!(env!("OUT_DIR"), "/conduit.common.rs"));
    }

    pub mod destination {
        include!(concat!(env!("OUT_DIR"), "/conduit.proxy.destination.rs"));
    }

    pub mod tap {
        include!(concat!(env!("OUT_DIR"), "/conduit.proxy.tap.rs"));
    }

    pub mod telemetry {
        include!(concat!(env!("OUT_DIR"), "/conduit.proxy.telemetry.rs"));
    }
}

fn pb_response_end(
    ctx: &Arc<ctx::http::Request>,
    since_request_init: ::std::time::Duration,
    since_response_init: Option<::std::time::Duration>,
    response_bytes: u64,
    grpc_status: u32,
) -> common::TapEvent {
    use self::common::tap_event;

    let end = tap_event::http::ResponseEnd {
        id: Some(tap_event::http::StreamId {
            base: 0, // TODO FIXME
            stream: ctx.id as u64,
        }),
        since_request_init: Some(pb_duration(&since_request_init)),
        since_response_init: since_response_init.as_ref().map(pb_duration),
        response_bytes,
        grpc_status,
    };

    common::TapEvent {
        source: Some((&ctx.server.remote).into()),
        target: Some((&ctx.client.remote).into()),
        event: Some(tap_event::Event::Http(tap_event::Http {
            event: Some(tap_event::http::Event::ResponseEnd(end)),
        })),
    }
}

#[derive(Debug, Clone)]
// TODO: do we want to carry the string if there is one?
pub struct InvalidMethod;

impl fmt::Display for InvalidMethod {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "invalid http method")
    }
}

impl Error for InvalidMethod {
    #[inline]
    fn description(&self) -> &str {
        "invalid http method"
    }
}

#[derive(Debug, Clone)]
pub struct InvalidScheme;

impl fmt::Display for InvalidScheme {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "invalid http scheme")
    }
}

impl Error for InvalidScheme {
    #[inline]
    fn description(&self) -> &str {
        "invalid http scheme"
    }
}

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
                        stream: ctx.id as u64,
                    }),
                    method: Some((&ctx.method).into()),
                    scheme: ctx.uri.scheme().map(|s| s.into()),
                    authority: ctx.uri
                        .authority_part()
                        .map(|a| a.as_str())
                        .unwrap_or_default()
                        .into(),
                    path: ctx.uri.path().into(),
                };

                common::TapEvent {
                    source: Some((&ctx.server.remote).into()),
                    target: Some((&ctx.client.remote).into()),
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
                        stream: ctx.request.id as u64,
                    }),
                    since_request_init: Some(pb_duration(&rsp.since_request_open)),
                    http_status: u32::from(ctx.status.as_u16()),
                };

                common::TapEvent {
                    source: Some((&ctx.request.server.remote).into()),
                    target: Some((&ctx.request.client.remote).into()),
                    event: Some(tap_event::Event::Http(tap_event::Http {
                        event: Some(tap_event::http::Event::ResponseInit(init)),
                    })),
                }
            }

            Event::StreamRequestFail(ref ctx, ref fail) => {
                pb_response_end(ctx, fail.since_request_open, None, 0, 0)
            }

            Event::StreamResponseEnd(ref ctx, ref end) => pb_response_end(
                &ctx.request,
                end.since_request_open,
                Some(end.since_response_open),
                end.bytes_sent,
                end.grpc_status.unwrap_or(0),
            ),

            Event::StreamResponseFail(ref ctx, ref fail) => pb_response_end(
                &ctx.request,
                fail.since_request_open,
                Some(fail.since_response_open),
                fail.bytes_sent,
                0,
            ),

            _ => return Err(UnknownEvent),
        };

        Ok(tap_ev)
    }
}

impl<'a> TryFrom<&'a common::http_method::Type> for http::Method {
    type Err = InvalidMethod;
    fn try_from(m: &'a common::http_method::Type) -> Result<Self, Self::Err> {
        use self::common::http_method::*;
        use http::HttpTryFrom;

        match *m {
            Type::Registered(reg) => if reg == Registered::Get.into() {
                Ok(http::Method::GET)
            } else if reg == Registered::Post.into() {
                Ok(http::Method::POST)
            } else if reg == Registered::Put.into() {
                Ok(http::Method::PUT)
            } else if reg == Registered::Delete.into() {
                Ok(http::Method::DELETE)
            } else if reg == Registered::Patch.into() {
                Ok(http::Method::PATCH)
            } else if reg == Registered::Options.into() {
                Ok(http::Method::OPTIONS)
            } else if reg == Registered::Connect.into() {
                Ok(http::Method::CONNECT)
            } else if reg == Registered::Head.into() {
                Ok(http::Method::HEAD)
            } else if reg == Registered::Trace.into() {
                Ok(http::Method::TRACE)
            } else {
                Err(InvalidMethod)
            },
            Type::Unregistered(ref m) => {
                HttpTryFrom::try_from(m.as_str()).map_err(|_| InvalidMethod)
            }
        }
    }
}

impl<'a> TryInto<String> for &'a common::scheme::Type {
    type Err = InvalidScheme;
    fn try_into(self) -> Result<String, Self::Err> {
        use self::common::scheme::*;

        match *self {
            Type::Registered(reg) => if reg == Registered::Http.into() {
                Ok("http".into())
            } else if reg == Registered::Https.into() {
                Ok("https".into())
            } else {
                Err(InvalidScheme)
            },
            Type::Unregistered(ref s) => Ok(s.clone()),
        }
    }
}

impl<'a> From<&'a http::Method> for common::http_method::Type {
    fn from(m: &'a http::Method) -> Self {
        use self::common::http_method::*;

        match *m {
            http::Method::GET => Type::Registered(Registered::Get.into()),
            http::Method::POST => Type::Registered(Registered::Post.into()),
            http::Method::PUT => Type::Registered(Registered::Put.into()),
            http::Method::DELETE => Type::Registered(Registered::Delete.into()),
            http::Method::HEAD => Type::Registered(Registered::Head.into()),
            http::Method::OPTIONS => Type::Registered(Registered::Options.into()),
            http::Method::CONNECT => Type::Registered(Registered::Connect.into()),
            http::Method::TRACE => Type::Registered(Registered::Trace.into()),
            ref method => Type::Unregistered(method.as_str().into()),
        }
    }
}

impl<'a> From<&'a http::Method> for common::HttpMethod {
    fn from(m: &'a http::Method) -> Self {
        common::HttpMethod {
            type_: Some(m.into()),
        }
    }
}

impl<'a> From<&'a str> for common::scheme::Type {
    fn from(s: &'a str) -> Self {
        use self::common::scheme::*;

        match s {
            "http" => Type::Registered(Registered::Http.into()),
            "https" => Type::Registered(Registered::Https.into()),
            s => Type::Unregistered(s.into()),
        }
    }
}

impl<'a> From<&'a str> for common::Scheme {
    fn from(s: &'a str) -> Self {
        common::Scheme {
            type_: Some(s.into()),
        }
    }
}

// ===== impl common::IpAddress =====

impl<T> From<T> for common::IpAddress
where
    common::ip_address::Ip: From<T>,
{
    #[inline]
    fn from(ip: T) -> Self {
        Self {
            ip: Some(ip.into()),
        }
    }
}

impl From<::std::net::IpAddr> for common::IpAddress {
    fn from(ip: ::std::net::IpAddr) -> Self {
        match ip {
            ::std::net::IpAddr::V4(v4) => Self {
                ip: Some(v4.into()),
            },
            ::std::net::IpAddr::V6(v6) => Self {
                ip: Some(v6.into()),
            },
        }
    }
}

// ===== impl common::IPv6 =====

impl From<[u8; 16]> for common::IPv6 {
    fn from(octets: [u8; 16]) -> Self {
        let first = (u64::from(octets[0]) << 56) + (u64::from(octets[1]) << 48)
            + (u64::from(octets[2]) << 40) + (u64::from(octets[3]) << 32)
            + (u64::from(octets[4]) << 24) + (u64::from(octets[5]) << 16)
            + (u64::from(octets[6]) << 8) + u64::from(octets[7]);
        let last = (u64::from(octets[8]) << 56) + (u64::from(octets[9]) << 48)
            + (u64::from(octets[10]) << 40) + (u64::from(octets[11]) << 32)
            + (u64::from(octets[12]) << 24) + (u64::from(octets[13]) << 16)
            + (u64::from(octets[14]) << 8) + u64::from(octets[15]);
        Self {
            first,
            last,
        }
    }
}

impl From<::std::net::Ipv6Addr> for common::IPv6 {
    #[inline]
    fn from(v6: ::std::net::Ipv6Addr) -> Self {
        Self::from(v6.octets())
    }
}

impl<'a> From<&'a common::IPv6> for ::std::net::Ipv6Addr {
    fn from(ip: &'a common::IPv6) -> ::std::net::Ipv6Addr {
        ::std::net::Ipv6Addr::new(
            (ip.first >> 48) as u16,
            (ip.first >> 32) as u16,
            (ip.first >> 16) as u16,
            (ip.first) as u16,
            (ip.last >> 48) as u16,
            (ip.last >> 32) as u16,
            (ip.last >> 16) as u16,
            (ip.last) as u16,
        )
    }
}

// ===== impl common::ip_address::Ip =====

impl From<[u8; 4]> for common::ip_address::Ip {
    fn from(octets: [u8; 4]) -> Self {
        common::ip_address::Ip::Ipv4(
            u32::from(octets[0]) << 24 | u32::from(octets[1]) << 16 | u32::from(octets[2]) << 8
                | u32::from(octets[3]),
        )
    }
}

impl From<::std::net::Ipv4Addr> for common::ip_address::Ip {
    #[inline]
    fn from(v4: ::std::net::Ipv4Addr) -> Self {
        Self::from(v4.octets())
    }
}

impl<T> From<T> for common::ip_address::Ip
where
    common::IPv6: From<T>,
{
    #[inline]
    fn from(t: T) -> Self {
        common::ip_address::Ip::Ipv6(common::IPv6::from(t))
    }
}


impl<'a> From<&'a ::std::net::SocketAddr> for common::TcpAddress {
    fn from(sa: &::std::net::SocketAddr) -> common::TcpAddress {
        common::TcpAddress {
            ip: Some(sa.ip().into()),
            port: u32::from(sa.port()),
        }
    }
}

impl hash::Hash for common::Protocol {
    // it's necessary to implement Hash for Protocol as it's a field on
    // ctx::Transport, which derives Hash.
    fn hash<H: hash::Hasher>(&self, state: &mut H) {
        (*self as i32).hash(state)
    }
}

fn pb_duration(d: &::std::time::Duration) -> ::prost_types::Duration {
    let seconds = if d.as_secs() > ::std::i64::MAX as u64 {
        ::std::i64::MAX
    } else {
        d.as_secs() as i64
    };

    let nanos = if d.subsec_nanos() > ::std::i32::MAX as u32 {
        ::std::i32::MAX
    } else {
        d.subsec_nanos() as i32
    };

    ::prost_types::Duration {
        seconds,
        nanos,
    }
}
