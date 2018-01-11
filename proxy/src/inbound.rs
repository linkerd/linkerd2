use std::io;
use std::net::{IpAddr, SocketAddr};
use std::sync::Arc;

use http;
use tokio_core::reactor::Handle;
use tower_buffer::{self, Buffer};
use tower_h2;
use tower_reconnect::{self, Reconnect};
use tower_router::Recognize;

use bind;
use ctx;
use telemetry;
use transport;

type Bind<B> = bind::Bind<Arc<ctx::Proxy>, B>;

pub struct Inbound<B> {
    default_addr: Option<SocketAddr>,
    bind: Bind<B>,
}

type Client<B> = tower_h2::client::Client<
    telemetry::sensor::Connect<transport::TimeoutConnect<transport::Connect>>,
    CtxtExec,
    B,
>;

type CtxtExec = ::logging::ContextualExecutor<(&'static str, SocketAddr), Handle>;

// ===== impl Inbound =====

impl<B> Inbound<B> {
    pub fn new(default_addr: Option<SocketAddr>, bind: Bind<B>) -> Self {
        Self {
            default_addr,
            bind,
        }
    }

    fn same_addr(a0: &SocketAddr, a1: &SocketAddr) -> bool {
        (a0.port() == a1.port()) && match (a0.ip(), a1.ip()) {
            (IpAddr::V6(a0), IpAddr::V4(a1)) => a0.to_ipv4() == Some(a1),
            (IpAddr::V4(a0), IpAddr::V6(a1)) => Some(a0) == a1.to_ipv4(),
            (a0, a1) => (a0 == a1),
        }
    }
}

impl<B> Recognize for Inbound<B>
where
    B: tower_h2::Body + 'static,
{
    type Request = http::Request<B>;
    type Response = http::Response<telemetry::sensor::http::ResponseBody<tower_h2::RecvBody>>;
    type Error = tower_buffer::Error<
        tower_reconnect::Error<
            tower_h2::client::Error,
            tower_h2::client::ConnectError<transport::TimeoutError<io::Error>>,
        >,
    >;
    type Key = SocketAddr;
    type RouteError = ();
    type Service = Buffer<Reconnect<telemetry::sensor::NewHttp<Client<B>, B, tower_h2::RecvBody>>>;

    fn recognize(&self, req: &Self::Request) -> Option<Self::Key> {
        let key = req.extensions()
            .get::<Arc<ctx::transport::Server>>()
            .and_then(|ctx| {
                trace!("recognize local={} orig={:?}", ctx.local, ctx.orig_dst);
                match ctx.orig_dst {
                    None => None,
                    Some(orig_dst) => {
                        // If the original destination is actually the listening socket,
                        // we don't want to create a loop.
                        if Self::same_addr(&orig_dst, &ctx.local) {
                            None
                        } else {
                            Some(orig_dst)
                        }
                    }
                }
            })
            .or_else(|| self.default_addr);

        trace!("recognize key={:?}", key);

        key
    }

    /// Builds a static service to a single endpoint.
    ///
    /// # TODO
    ///
    /// Buffering is currently unbounded and does not apply timeouts. This must be
    /// changed.
    fn bind_service(&mut self, addr: &SocketAddr) -> Result<Self::Service, Self::RouteError> {
        debug!("building inbound client to {}", addr);

        // Wrap with buffering. This currently is an unbounded buffer, which
        // is not ideal.
        //
        // TODO: Don't use unbounded buffering.
        Buffer::new(self.bind.bind_service(addr), self.bind.executor()).map_err(|_| {})
    }
}

#[cfg(test)]
mod tests {
    use std::net;
    use std::sync::Arc;

    use http;
    use quickcheck::TestResult;
    use tokio_core::reactor::Core;
    use tower_router::Recognize;

    use super::Inbound;
    use control::pb::common::Protocol;
    use bind::Bind;
    use ctx;

    fn new_inbound(default: Option<net::SocketAddr>, ctx: &Arc<ctx::Proxy>) -> Inbound<()> {
        let core = Core::new().unwrap();
        let bind = Bind::new(core.handle()).with_ctx(ctx.clone());
        Inbound::new(default, bind)
    }

    quickcheck! {
        fn same_addr_ipv4(ip0: net::Ipv4Addr, ip1: net::Ipv4Addr, port0: u16, port1: u16) -> TestResult {
            if port0 == 0 || port0 == ::std::u16::MAX {
                return TestResult::discard();
            } else if port1 == 0 || port1 == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr0 = net::SocketAddr::new(net::IpAddr::V4(ip0), port0);
            let addr1 = net::SocketAddr::new(net::IpAddr::V4(ip1), port1);
            TestResult::from_bool(Inbound::<()>::same_addr(&addr0, &addr1) == (addr0 == addr1))
        }

        fn same_addr_ipv6(ip0: net::Ipv6Addr, ip1: net::Ipv6Addr, port0: u16, port1: u16) -> TestResult {
            if port0 == 0 || port0 == ::std::u16::MAX {
                return TestResult::discard();
            } else if port1 == 0 || port1 == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr0 = net::SocketAddr::new(net::IpAddr::V6(ip0), port0);
            let addr1 = net::SocketAddr::new(net::IpAddr::V6(ip1), port1);
            TestResult::from_bool(Inbound::<()>::same_addr(&addr0, &addr1) == (addr0 == addr1))
        }

        fn same_addr_ip6_mapped_ipv4(ip: net::Ipv4Addr, port: u16) -> TestResult {
            if port == 0 || port == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr4 = net::SocketAddr::new(net::IpAddr::V4(ip), port);
            let addr6 = net::SocketAddr::new(net::IpAddr::V6(ip.to_ipv6_mapped()), port);
            TestResult::from_bool(Inbound::<()>::same_addr(&addr4, &addr6))
        }

        fn same_addr_ip6_compat_ipv4(ip: net::Ipv4Addr, port: u16) -> TestResult {
            if port == 0 || port == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr4 = net::SocketAddr::new(net::IpAddr::V4(ip), port);
            let addr6 = net::SocketAddr::new(net::IpAddr::V6(ip.to_ipv6_compatible()), port);
            TestResult::from_bool(Inbound::<()>::same_addr(&addr4, &addr6))
        }

        fn recognize_orig_dst(
            orig_dst: net::SocketAddr,
            local: net::SocketAddr,
            remote: net::SocketAddr
        ) -> bool {
            let ctx = ctx::Proxy::inbound(&ctx::Process::test("test", "test", "test"));

            let inbound = new_inbound(None, &ctx);

            let mut req = http::Request::new(());
            req.extensions_mut()
                .insert(ctx::transport::Server::new(
                    &ctx,
                    &local,
                    &remote,
                    &Some(orig_dst),
                    Protocol::Http,
                ));

            let rec = if Inbound::<()>::same_addr(&orig_dst, &local) {
                None
            } else {
                Some(orig_dst)
            };

            inbound.recognize(&req) == rec
        }

        fn recognize_default_no_orig_dst(
            default: Option<net::SocketAddr>,
            local: net::SocketAddr,
            remote: net::SocketAddr
        ) -> bool {
            let ctx = ctx::Proxy::inbound(&ctx::Process::test("test", "test", "test"));

            let inbound = new_inbound(default, &ctx);

            let mut req = http::Request::new(());
            req.extensions_mut()
                .insert(ctx::transport::Server::new(
                    &ctx,
                    &local,
                    &remote,
                    &None,
                    Protocol::Http,
                ));

            inbound.recognize(&req) == default
        }

        fn recognize_default_no_ctx(default: Option<net::SocketAddr>) -> bool {
            let ctx = ctx::Proxy::inbound(&ctx::Process::test("test", "test", "test"));

            let inbound = new_inbound(default, &ctx);

            let req = http::Request::new(());

            inbound.recognize(&req) == default
        }

        fn recognize_default_no_loop(
            default: Option<net::SocketAddr>,
            local: net::SocketAddr,
            remote: net::SocketAddr
        ) -> bool {
            let ctx = ctx::Proxy::inbound(&ctx::Process::test("test", "test", "test"));

            let inbound = new_inbound(default, &ctx);

            let mut req = http::Request::new(());
            req.extensions_mut()
                .insert(ctx::transport::Server::new(
                    &ctx,
                    &local,
                    &remote,
                    &Some(local),
                    Protocol::Http,
                ));

            inbound.recognize(&req) == default
        }
    }
}
