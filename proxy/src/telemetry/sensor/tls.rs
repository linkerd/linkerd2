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
    pub(super) ctx: AcceptCtx,
}

#[derive(Clone, Debug)]
pub struct Connect {
    pub(super) handle: super::Handle,
    pub(super) ctx: ConnectCtx,
}

#[derive(Clone, Debug)]
pub struct AcceptHandshakeFuture<F> {
    inner: F,
    handle: super::Handle,
    ctx: AcceptCtx,
    remote_addr: SocketAddr,
    local_addr: SocketAddr,
}


#[derive(Clone, Debug)]
pub(super) enum AcceptCtx {
    Proxy(Arc<ctx::Proxy>),
    Control,
}

#[derive(Clone, Debug)]
pub(super) enum ConnectCtx {
    Proxy(Arc<ctx::transport::Client>),
    Control { remote_addr: SocketAddr, },
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
            ctx: self.ctx.clone(),
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
            let event = match self.ctx {
                AcceptCtx::Proxy(ref proxy_ctx) => {
                    let ctx = ctx::transport::Server::new(
                        proxy_ctx,
                        &self.local_addr,
                        &self.remote_addr,
                        &None, // we haven't determined the original dst yet.
                        Conditional::None(tls::ReasonForNoTls::HandshakeFailed),
                    );
                    // XXX: this arc is unnecessary...
                    let ctx = Arc::new(ctx::transport::Ctx::Server(ctx));
                    event::Event::TlsHandshakeFailed(ctx, e.into())
                },
                AcceptCtx::Control =>{
                    event::Event::ControlTlsHandshakeFailed(
                        event::ControlConnection::Accept {
                            local_addr: self.local_addr,
                            remote_addr: self.remote_addr,
                        },
                        e.into(),
                    )
                },
            };

            self.handle.send(|| event);
        };
        poll
    }
}

impl Connect {
    pub fn fail(&mut self, error: &io::Error) {
        let event = match self.ctx {
            ConnectCtx::Proxy(ref proxy_ctx) => {
                let ctx = Arc::new(ctx::transport::Ctx::Client(
                    proxy_ctx.with_tls_status(
                        Conditional::None(tls::ReasonForNoTls::HandshakeFailed)
                    )
                ));
                event::Event::TlsHandshakeFailed(ctx, error.into())
            },
            ConnectCtx::Control { remote_addr } => {
                event::Event::ControlTlsHandshakeFailed(
                    event::ControlConnection::Connect { remote_addr },
                    error.into(),
                )
            },
        };

        self.handle.send(|| event);
    }
}
