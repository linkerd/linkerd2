#![allow(dead_code)]
#![cfg_attr(feature = "cargo-clippy", allow(clippy))]

extern crate convert;
extern crate h2;
extern crate http;
extern crate prost;
#[macro_use]
extern crate prost_derive;
extern crate prost_types;
#[cfg(feature = "arbitrary")]
extern crate quickcheck;
extern crate tower_grpc;

use convert::{TryFrom, TryInto};
use std::{fmt, hash};
use std::error::Error;

pub use self::proto::*;

// The generated code requires two tiers of outer modules so that references between
// modules resolve properly.
mod proto {
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

// ===== impl common::Eos =====

impl From<h2::Reason> for common::Eos {
    fn from(reason: h2::Reason) -> Self {
        let end = common::eos::End::ResetErrorCode(reason.into());
        common::Eos { end: Some(end) }
    }
}

impl common::Eos {
    pub fn from_grpc_status(code: u32) -> Self {
        let end = common::eos::End::GrpcStatusCode(code);
        common::Eos { end: Some(end) }
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

pub fn pb_duration(d: &::std::time::Duration) -> ::prost_types::Duration {
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

impl<'a> From<&'a http::uri::Scheme> for common::Scheme {
    fn from(scheme: &'a http::uri::Scheme) -> Self {
        scheme.as_ref().into()
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

#[cfg(feature = "arbitrary")]
pub mod arbitrary {
    use std::boxed::Box;

    use quickcheck::*;

    use super::common::*;
    use super::tap::*;

    impl Arbitrary for ObserveRequest {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            ObserveRequest {
                limit: g.gen(),
                match_: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for observe_request::Match {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            observe_request::Match {
                match_: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for observe_request::match_::Match {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            match g.gen::<u32>() % 6 {
                0 => observe_request::match_::Match::All(Arbitrary::arbitrary(g)),
                1 => observe_request::match_::Match::Any(Arbitrary::arbitrary(g)),
                2 => observe_request::match_::Match::Not(Box::new(Arbitrary::arbitrary(g))),
                3 => observe_request::match_::Match::Source(Arbitrary::arbitrary(g)),
                4 => observe_request::match_::Match::Destination(Arbitrary::arbitrary(g)),
                5 => observe_request::match_::Match::Http(Arbitrary::arbitrary(g)),
                _ => unreachable!(),
            }
        }
    }

    impl Arbitrary for observe_request::match_::Seq {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            observe_request::match_::Seq {
                matches: Arbitrary::arbitrary(g),
            }
        }

        fn shrink(&self) -> Box<Iterator<Item = Self>> {
            Box::new(self.matches.shrink().map(|matches| {
                observe_request::match_::Seq {
                    matches,
                }
            }))
        }
    }

    impl Arbitrary for observe_request::match_::Tcp {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            observe_request::match_::Tcp {
                match_: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for observe_request::match_::tcp::Match {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            use self::observe_request::match_::tcp;

            if g.gen::<bool>() {
                tcp::Match::Netmask(Arbitrary::arbitrary(g))
            } else {
                tcp::Match::Ports(Arbitrary::arbitrary(g))
            }
        }
    }

    impl Arbitrary for observe_request::match_::tcp::PortRange {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            observe_request::match_::tcp::PortRange {
                min: g.gen(),
                max: g.gen(),
            }
        }
    }

    impl Arbitrary for observe_request::match_::tcp::Netmask {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            let ip: Option<IpAddress> = Arbitrary::arbitrary(g);
            let mask = match ip.as_ref().and_then(|a| a.ip.as_ref()) {
                Some(&ip_address::Ip::Ipv4(_)) => g.gen::<u32>() % 32 + 1,
                Some(&ip_address::Ip::Ipv6(_)) => g.gen::<u32>() % 128 + 1,
                None => 0u32,
            };
            observe_request::match_::tcp::Netmask {
                ip,
                mask,
            }
        }
    }

    impl Arbitrary for observe_request::match_::Http {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            observe_request::match_::Http {
                match_: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for observe_request::match_::http::Match {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            use self::observe_request::match_::http;

            match g.gen::<u32>() % 4 {
                0 => http::Match::Scheme(Scheme::arbitrary(g)),
                1 => http::Match::Method(HttpMethod::arbitrary(g)),
                2 => http::Match::Authority(http::StringMatch::arbitrary(g)),
                3 => http::Match::Path(http::StringMatch::arbitrary(g)),
                _ => unreachable!(),
            }
        }
    }

    impl Arbitrary for observe_request::match_::http::StringMatch {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            observe_request::match_::http::StringMatch {
                match_: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for observe_request::match_::http::string_match::Match {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            use self::observe_request::match_::http::string_match;

            match g.gen::<u32>() % 2 {
                0 => string_match::Match::Exact(String::arbitrary(g)),
                1 => string_match::Match::Prefix(String::arbitrary(g)),
                _ => unreachable!(),
            }
        }
    }

    impl Arbitrary for IpAddress {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            IpAddress {
                ip: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for ip_address::Ip {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            if g.gen::<bool>() {
                ip_address::Ip::Ipv4(g.gen())
            } else {
                ip_address::Ip::Ipv6(IPv6::arbitrary(g))
            }
        }
    }

    impl Arbitrary for IPv6 {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            IPv6 {
                first: g.gen(),
                last: g.gen(),
            }
        }
    }

    impl Arbitrary for HttpMethod {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            HttpMethod {
                type_: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for http_method::Type {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            match g.gen::<u16>() % 9 {
                8 => http_method::Type::Unregistered(String::arbitrary(g)),
                n => http_method::Type::Registered(i32::from(n).into()),
            }
        }
    }

    impl Arbitrary for Scheme {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            Scheme {
                type_: Arbitrary::arbitrary(g),
            }
        }
    }

    impl Arbitrary for scheme::Type {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            match g.gen::<u16>() % 3 {
                3 => scheme::Type::Unregistered(String::arbitrary(g)),
                n => scheme::Type::Registered(i32::from(n).into()),
            }
        }
    }
}
