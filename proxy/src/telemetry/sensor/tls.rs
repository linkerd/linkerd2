use std::{
    io,
    net::SocketAddr,
    sync::Arc,
};

use futures::{Future, Poll};

use ctx;
use conditional::Conditional;
use telemetry::event;
use tls;

#[derive(Clone, Debug)]
pub struct Accept {
    pub(super) local_addr: SocketAddr,
    pub(super) handle: super::Handle,
    pub(super) ctx: Arc<ctx::Proxy>,
}

#[derive(Clone, Debug)]
pub struct Connect {
    pub(super) handle: super::Handle,
    pub(super) ctx: Arc<ctx::transport::Client>,
}

#[derive(Clone, Debug)]
pub struct AcceptHandshakeFuture<F> {
    inner: F,
    handle: super::Handle,
    proxy_ctx: Arc<ctx::Proxy>,
    remote_addr: SocketAddr,
    local_addr: SocketAddr,
}

impl Accept {
    pub fn accept<F: Future<Error = ::io::Error>>(
        &self,
        remote_addr: SocketAddr,
        inner: F,
    ) -> AcceptHandshakeFuture<F> {
        AcceptHandshakeFuture {
            inner,
            handle: self.handle.clone(),
            proxy_ctx: self.ctx.clone(),
            local_addr: self.local_addr,
            remote_addr,
        }
    }

}

impl<F: Future<Error = ::io::Error>> Future for AcceptHandshakeFuture<F> {
    type Item = F::Item;
    type Error = F::Error;
    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let poll = self.inner.poll();
        if let Err(ref e) = poll {
            let ctx = ctx::transport::Server::new(
                &self.proxy_ctx,
                &self.local_addr,
                &self.remote_addr,
                &None, // we haven't determined the original dst yet.
                Conditional::None(tls::ReasonForNoTls::HandshakeFailed),
            );
            // XXX: this arc is unnecessary...Connect
            let ctx = Arc::new(ctx::transport::Ctx::Server(ctx));
            self.handle.send(|| {
                event::Event::TlsHandshakeFailed(ctx, e.into())
            });
        };
        poll
    }
}

impl Connect {
    pub fn fail(&mut self, error: &io::Error) {
        let ctx = Arc::new(ctx::transport::Ctx::Client(
            self.ctx.with_tls_status(
                Conditional::None(tls::ReasonForNoTls::HandshakeFailed)
            )
        ));
        self.handle.send(|| {
            event::Event::TlsHandshakeFailed(ctx, error.into())
        });
    }
}
