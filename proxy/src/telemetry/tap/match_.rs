use std::boxed::Box;
use std::net;
use std::sync::Arc;

use http;
use ipnet::{Contains, Ipv4Net, Ipv6Net};

use super::Event;
use conduit_proxy_controller_grpc::common::ip_address;
use conduit_proxy_controller_grpc::tap::observe_request;
use convert::*;
use ctx;

#[derive(Clone, Debug)]
pub(super) enum Match {
    Any(Vec<Match>),
    All(Vec<Match>),
    Not(Box<Match>),
    Source(TcpMatch),
    Destination(TcpMatch),
    Http(HttpMatch),
}

#[derive(Eq, PartialEq)]
pub enum InvalidMatch {
    Empty,
    InvalidPort,
    InvalidNetwork,
    InvalidHttpMethod,
    InvalidScheme,
}

#[derive(Clone, Debug)]
pub(super) enum TcpMatch {
    // Inclusive
    PortRange(u16, u16),
    Net(NetMatch),
}

#[derive(Clone, Debug)]
pub(super) enum NetMatch {
    Net4(Ipv4Net),
    Net6(Ipv6Net),
}

#[derive(Clone, Debug)]
pub(super) enum HttpMatch {
    Scheme(String),
    Method(http::Method),
    Path(observe_request::match_::http::string_match::Match),
    Authority(observe_request::match_::http::string_match::Match),
}

// ===== impl Match ======

impl Match {
    pub(super) fn matches(&self, ev: &Event) -> bool {
        match *self {
            Match::Any(ref any) => {
                for m in any {
                    if m.matches(ev) {
                        return true;
                    }
                }
                false
            }

            Match::All(ref all) => {
                for m in all {
                    if !m.matches(ev) {
                        return false;
                    }
                }
                true
            }

            Match::Not(ref not) => !not.matches(ev),

            Match::Source(ref src) => match *ev {
                Event::StreamRequestOpen(ref req) | Event::StreamRequestFail(ref req, _) => {
                    src.matches(&req.server.remote)
                }
                Event::StreamResponseOpen(ref rsp, _) |
                Event::StreamResponseFail(ref rsp, _) |
                Event::StreamResponseEnd(ref rsp, _) => src.matches(&rsp.request.server.remote),
                _ => false,
            },

            Match::Destination(ref dst) => match *ev {
                Event::StreamRequestOpen(ref req) | Event::StreamRequestFail(ref req, _) => {
                    dst.matches(&req.client.remote)
                }
                Event::StreamResponseOpen(ref rsp, _) |
                Event::StreamResponseFail(ref rsp, _) |
                Event::StreamResponseEnd(ref rsp, _) => dst.matches(&rsp.request.client.remote),
                _ => false,
            },

            Match::Http(ref http) => match *ev {
                Event::StreamRequestOpen(ref req) | Event::StreamRequestFail(ref req, _) => {
                    http.matches(req)
                }

                Event::StreamResponseOpen(ref rsp, _) |
                Event::StreamResponseFail(ref rsp, _) |
                Event::StreamResponseEnd(ref rsp, _) => http.matches(&rsp.request),

                _ => false,
            },
        }
    }

    pub(super) fn new(match_: &observe_request::Match) -> Result<Match, InvalidMatch> {
        match_
            .match_
            .as_ref()
            .map(Match::try_from)
            .unwrap_or_else(|| Err(InvalidMatch::Empty))
    }

    fn from_seq(seq: &observe_request::match_::Seq) -> Result<Vec<Match>, InvalidMatch> {
        let mut new = Vec::with_capacity(seq.matches.len());

        for m in &seq.matches {
            if let Some(m) = m.match_.as_ref() {
                new.push(Self::try_from(m)?);
            }
        }

        Ok(new)
    }
}

impl<'a> TryFrom<&'a observe_request::match_::Match> for Match {
    type Err = InvalidMatch;

    #[allow(unconditional_recursion)]
    fn try_from(m: &observe_request::match_::Match) -> Result<Self, Self::Err> {
        use conduit_proxy_controller_grpc::tap::observe_request::match_;

        let match_ = match *m {
            match_::Match::All(ref seq) => Match::All(Self::from_seq(seq)?),

            match_::Match::Any(ref seq) => Match::Any(Self::from_seq(seq)?),

            match_::Match::Not(ref m) => match m.match_.as_ref() {
                Some(m) => Match::Not(Box::new(Self::try_from(m)?)),
                None => return Err(InvalidMatch::Empty),
            },

            match_::Match::Source(ref src) => Match::Source(TcpMatch::try_from(src)?),

            match_::Match::Destination(ref dst) => Match::Destination(TcpMatch::try_from(dst)?),

            match_::Match::Http(ref http) => Match::Http(HttpMatch::try_from(http)?),
        };

        Ok(match_)
    }
}

// ===== impl TcpMatch ======

impl TcpMatch {
    fn matches(&self, addr: &net::SocketAddr) -> bool {
        match *self {
            // If either a minimum or maximum is not specified, the range is considered to
            // be over a discrete value.
            TcpMatch::PortRange(min, max) => min <= addr.port() && addr.port() <= max,

            TcpMatch::Net(ref net) => net.matches(&addr.ip()),
        }
    }
}

impl<'a> TryFrom<&'a observe_request::match_::Tcp> for TcpMatch {
    type Err = InvalidMatch;

    fn try_from(m: &observe_request::match_::Tcp) -> Result<Self, InvalidMatch> {
        use conduit_proxy_controller_grpc::tap::observe_request::match_::tcp;

        let m = match m.match_.as_ref() {
            None => return Err(InvalidMatch::Empty),
            Some(m) => m,
        };

        let match_ = match *m {
            tcp::Match::Ports(ref range) => {
                // If either a minimum or maximum is not specified, the range is considered to
                // be over a discrete value.
                let min = if range.min == 0 { range.max } else { range.min };
                let max = if range.max == 0 { range.min } else { range.max };
                if min == 0 || max == 0 {
                    return Err(InvalidMatch::Empty);
                }
                if min > u32::from(::std::u16::MAX) || max > u32::from(::std::u16::MAX) {
                    return Err(InvalidMatch::InvalidPort);
                }
                TcpMatch::PortRange(min as u16, max as u16)
            }

            tcp::Match::Netmask(ref netmask) => TcpMatch::Net(NetMatch::try_from(netmask)?),
        };

        Ok(match_)
    }
}

// ===== impl NetMatch ======

impl NetMatch {
    fn matches(&self, addr: &net::IpAddr) -> bool {
        match *self {
            NetMatch::Net4(ref net) => match *addr {
                net::IpAddr::V6(_) => false,
                net::IpAddr::V4(ref addr) => net.contains(addr),
            },
            NetMatch::Net6(ref net) => match *addr {
                net::IpAddr::V4(_) => false,
                net::IpAddr::V6(ref addr) => net.contains(addr),
            },
        }
    }
}

impl<'a> TryFrom<&'a observe_request::match_::tcp::Netmask> for NetMatch {
    type Err = InvalidMatch;
    fn try_from(m: &'a observe_request::match_::tcp::Netmask) -> Result<Self, InvalidMatch> {
        let mask = if m.mask == 0 {
            return Err(InvalidMatch::Empty);
        } else if m.mask > u32::from(::std::u8::MAX) {
            return Err(InvalidMatch::InvalidNetwork);
        } else {
            m.mask as u8
        };

        let ip = match m.ip.as_ref().and_then(|a| a.ip.as_ref()) {
            Some(ip) => ip,
            None => return Err(InvalidMatch::Empty),
        };

        let net = match *ip {
            ip_address::Ip::Ipv4(ref n) => {
                let net =
                    Ipv4Net::new((*n).into(), mask).map_err(|_| InvalidMatch::InvalidNetwork)?;
                NetMatch::Net4(net)
            }
            ip_address::Ip::Ipv6(ref ip6) => {
                let net = Ipv6Net::new(ip6.into(), mask).map_err(|_| InvalidMatch::InvalidNetwork)?;
                NetMatch::Net6(net)
            }
        };

        Ok(net)
    }
}

// ===== impl HttpMatch ======

impl HttpMatch {
    fn matches(&self, req: &Arc<ctx::http::Request>) -> bool {
        match *self {
            HttpMatch::Scheme(ref m) => req.uri
                .scheme_part()
                .map(|s| m == s.as_ref())
                .unwrap_or(false),

            HttpMatch::Method(ref m) => *m == req.method,

            HttpMatch::Authority(ref m) => req.uri
                .authority_part()
                .map(|a| Self::matches_string(m, a.as_str()))
                .unwrap_or(false),

            HttpMatch::Path(ref m) => Self::matches_string(m, req.uri.path()),
        }
    }

    fn matches_string(
        string_match: &observe_request::match_::http::string_match::Match,
        value: &str,
    ) -> bool {
        use conduit_proxy_controller_grpc::tap::observe_request::match_::http::string_match::Match::*;

        match *string_match {
            Exact(ref exact) => value == exact,
            Prefix(ref prefix) => value.starts_with(prefix),
        }
    }
}

impl<'a> TryFrom<&'a observe_request::match_::Http> for HttpMatch {
    type Err = InvalidMatch;
    fn try_from(m: &'a observe_request::match_::Http) -> Result<Self, InvalidMatch> {
        use conduit_proxy_controller_grpc::tap::observe_request::match_::http::Match as Pb;

        m.match_
            .as_ref()
            .ok_or_else(|| InvalidMatch::Empty)
            .and_then(|m| match *m {
                Pb::Scheme(ref s) => s.type_
                    .as_ref()
                    .ok_or_else(|| InvalidMatch::Empty)
                    .and_then(|s| {
                        s.try_into()
                            .map(HttpMatch::Scheme)
                            .map_err(|_| InvalidMatch::InvalidScheme)
                    }),

                Pb::Method(ref m) => m.type_
                    .as_ref()
                    .ok_or_else(|| InvalidMatch::Empty)
                    .and_then(|m| {
                        m.try_into()
                            .map(HttpMatch::Method)
                            .map_err(|_| InvalidMatch::InvalidHttpMethod)
                    }),

                Pb::Authority(ref a) => a.match_
                    .as_ref()
                    .ok_or_else(|| InvalidMatch::Empty)
                    .map(|a| HttpMatch::Authority(a.clone())),

                Pb::Path(ref p) => p.match_
                    .as_ref()
                    .ok_or_else(|| InvalidMatch::Empty)
                    .map(|p| HttpMatch::Path(p.clone())),
            })
    }
}

#[cfg(test)]
mod tests {
    use ipnet::{Contains, Ipv4Net, Ipv6Net};
    use quickcheck::*;

    use super::*;
    use conduit_proxy_controller_grpc::*;

    impl Arbitrary for TcpMatch {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            if g.gen::<bool>() {
                TcpMatch::Net(NetMatch::arbitrary(g))
            } else {
                TcpMatch::PortRange(g.gen(), g.gen())
            }
        }
    }

    impl Arbitrary for NetMatch {
        fn arbitrary<G: Gen>(g: &mut G) -> Self {
            if g.gen::<bool>() {
                let addr = net::Ipv4Addr::arbitrary(g);
                let bits = g.gen::<u8>() % 32;
                let net = Ipv4Net::new(addr, bits).expect("ipv4 network address");
                NetMatch::Net4(net)
            } else {
                let addr = net::Ipv6Addr::arbitrary(g);
                let bits = g.gen::<u8>() % 128;
                let net = Ipv6Net::new(addr, bits).expect("ipv6 network address");
                NetMatch::Net6(net)
            }
        }
    }
    quickcheck! {
        fn tcp_from_proto(tcp: observe_request::match_::Tcp) -> bool {
            use self::observe_request::match_::tcp;

            let err: Option<InvalidMatch> =
                tcp.match_.as_ref()
                    .map(|m| match *m {
                        tcp::Match::Ports(ref ps) => {
                            let ok = 0 < ps.min &&
                                ps.min <= ps.max &&
                                ps.max < u32::from(::std::u16::MAX);
                            if ok { None } else { Some(InvalidMatch::InvalidPort) }
                        }
                        tcp::Match::Netmask(ref n) => {
                            let ip = n.ip.as_ref().and_then(|a| a.ip.as_ref());
                            if ip.is_some() { None } else { Some(InvalidMatch::Empty) }
                        }
                    })
                    .unwrap_or(Some(InvalidMatch::Empty));

            err == TcpMatch::try_from(&tcp).err()
        }

        fn tcp_matches(m: TcpMatch, addr: net::SocketAddr) -> bool {
            let matches = match (&m, addr.ip()) {
                (&TcpMatch::Net(NetMatch::Net4(ref n)), net::IpAddr::V4(ip)) => {
                    n.contains(&ip)
                }
                (&TcpMatch::Net(NetMatch::Net6(ref n)), net::IpAddr::V6(ip)) => {
                    n.contains(&ip)
                }
                (&TcpMatch::PortRange(min, max), _) => {
                    min <= addr.port() && addr.port() <= max
                }
                _ => false
            };

            m.matches(&addr) == matches
        }

        fn http_from_proto(http: observe_request::match_::Http) -> bool {
            use self::observe_request::match_::http;

            let err = match http.match_.as_ref() {
                None => Some(InvalidMatch::Empty),
                Some(&http::Match::Method(ref m)) => {
                    match m.type_.as_ref() {
                        None => Some(InvalidMatch::Empty),
                        Some(&common::http_method::Type::Unregistered(ref m)) => if m.len() <= 15 {
                            let mut err = None;
                            for c in m.bytes() {
                                let ok =
                                    b'A' <= c && c <= b'Z' ||
                                    b'a' <= c && c <= b'z' ||
                                    b'0' <= c && c <= b'9' ;
                                if !ok {
                                    err = Some(InvalidMatch::InvalidHttpMethod);
                                    break;
                                }
                            }
                            err
                        } else {
                            Some(InvalidMatch::InvalidHttpMethod)
                        }
                        Some(&common::http_method::Type::Registered(m)) => if m < 9 {
                            None
                        } else {
                            Some(InvalidMatch::InvalidHttpMethod)
                        }
                    }
                }
                Some(&http::Match::Scheme(ref m)) => {
                    match m.type_.as_ref() {
                        None => Some(InvalidMatch::Empty),
                        Some(&common::scheme::Type::Unregistered(_)) => None,
                        Some(&common::scheme::Type::Registered(m)) => {
                            if m < 2 {
                                None
                            } else {
                                Some(InvalidMatch::InvalidScheme)
                            }
                        }
                    }
                }
                Some(&http::Match::Authority(ref m)) => {
                    match m.match_ {
                        None => Some(InvalidMatch::Empty),
                        Some(_) => None,
                    }
                }
                Some(&http::Match::Path(ref m)) => {
                    match m.match_ {
                        None => Some(InvalidMatch::Empty),
                        Some(_) => None,
                    }
                }
            };

            err == HttpMatch::try_from(&http).err()
        }

        // TODO
        // fn http_matches(m: HttpMatch, ctx: Arc<ctx::http::Request>) -> bool {
        //     let matches = false;
        //     m.matches(&addr) == matches
        // }
    }
}
