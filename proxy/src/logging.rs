use std::cell::RefCell;
use std::env;
use std::io::Write;
use std::fmt;
use std::net::SocketAddr;
use std::sync::Arc;

use env_logger;
use futures::{Future, Poll};
use futures::future::{ExecuteError, Executor};
use log::{Level};

const ENV_LOG: &str = "CONDUIT_PROXY_LOG";

thread_local! {
    static CONTEXT: RefCell<Vec<*const fmt::Display>> = RefCell::new(Vec::new());
}

pub fn init() {
    env_logger::Builder::new()
        .format(|fmt, record| {
            CONTEXT.with(|ctxt| {
                let level = match record.level() {
                    Level::Trace => "TRCE",
                    Level::Debug => "DBUG",
                    Level::Info => "INFO",
                    Level::Warn => "WARN",
                    Level::Error => "ERR!",
                };
                writeln!(
                   fmt,
                    "{} {}{} {}",
                    level,
                    Context(&ctxt.borrow()),
                    record.target(),
                    record.args()
                )
            })
        })
        .parse(&env::var(ENV_LOG).unwrap_or_default())
        .init();
}

/// Execute a closure with a `Display` item attached to allow log messages.
pub fn context<T, F, U>(context: &T, mut closure: F) -> U
where
    T: fmt::Display + 'static,
    F: FnMut() -> U,
{
    let _guard = ContextGuard::new(context);
    closure()
}

/// Wrap a `Future` with a `Display` value that will be inserted into all logs
/// created by this Future.
pub fn context_future<T: fmt::Display, F: Future>(context: T, future: F) -> ContextualFuture<T, F> {
    ContextualFuture {
        context,
        future: Some(future),
    }
}

/// Wrap `task::LazyExecutor` to spawn futures that have a reference to the `Display`
/// value, inserting it into all logs created by this future.
pub fn context_executor<T: fmt::Display>(context: T) -> ContextualExecutor<T> {
    ContextualExecutor {
        context: Arc::new(context),
    }
}

#[derive(Debug)]
pub struct ContextualFuture<T: fmt::Display + 'static, F: Future> {
    context: T,
    future: Option<F>,
}

impl<T, F> Future for ContextualFuture<T, F>
where
    T: fmt::Display + 'static,
    F: Future,
{
    type Item = F::Item;
    type Error = F::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let ctxt = &self.context;
        let fut = self.future.as_mut().expect("poll after drop");
        context(ctxt, || fut.poll())
    }
}
impl<T, F> Drop for ContextualFuture<T, F>
where
    T: fmt::Display + 'static,
    F: Future,
{
    fn drop(&mut self) {
        if self.future.is_some() {
            let ctxt = &self.context;
            let fut = &mut self.future;
            context(ctxt, || drop(fut.take()))
        }
    }
}

#[derive(Debug)]
pub struct ContextualExecutor<T> {
    context: Arc<T>,
}

impl<T> ::tokio::executor::Executor for ContextualExecutor<T>
where
    T: fmt::Display + 'static + Send + Sync,
{
    fn spawn(
        &mut self,
        future: Box<Future<Item = (), Error = ()> + 'static + Send>
    ) -> ::std::result::Result<(), ::tokio::executor::SpawnError> {
        let fut = context_future(self.context.clone(), future);
        ::task::LazyExecutor.spawn(Box::new(fut))
    }
}

impl<T, F> Executor<F> for ContextualExecutor<T>
where
    T: fmt::Display + 'static + Send + Sync,
    F: Future<Item = (), Error = ()> + 'static + Send,
{
    fn execute(&self, future: F) -> ::std::result::Result<(), ExecuteError<F>> {
        let fut = context_future(self.context.clone(), future);
        match ::task::LazyExecutor.execute(fut) {
            Ok(()) => Ok(()),
            Err(err) => {
                let kind = err.kind();
                let mut future = err.into_future();
                Err(ExecuteError::new(kind, future.future.take().expect("future")))
            }
        }
    }
}

impl<T> Clone for ContextualExecutor<T> {
    fn clone(&self) -> Self {
        Self {
            context: self.context.clone(),
        }
    }
}

struct Context<'a>(&'a [*const fmt::Display]);

impl<'a> fmt::Display for Context<'a> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        if self.0.is_empty() {
            return Ok(());
        }

        for item in self.0 {
            // See `fn context()` for comments about this unsafe.
            let item = unsafe { &**item };
            write!(f, "{} ", item)?;
        }
        Ok(())
    }
}

/// Guards that the pushed context is removed from TLS afterwards.
///
/// Specifically, this protects even if the passed function panics,
/// as destructors are run while unwinding.
struct ContextGuard<'a>(&'a (fmt::Display + 'static));

impl<'a> ContextGuard<'a> {
    fn new(context: &'a (fmt::Display + 'static)) -> Self {
        // This is a raw pointer because of lifetime conflicts that require
        // the thread local to have a static lifetime.
        //
        // We don't want to require a static lifetime, and in fact,
        // only use the reference within this closure, so converting
        // to a raw pointer is safe.
        let raw = context as *const fmt::Display;
        CONTEXT.with(|ctxt| {
            ctxt.borrow_mut().push(raw);
        });
        ContextGuard(context)
    }
}

impl<'a> Drop for ContextGuard<'a> {
    fn drop(&mut self) {
        CONTEXT.with(|ctxt| {
            ctxt.borrow_mut().pop();
        });
    }
}

pub fn admin() -> Section {
    Section::Admin
}

pub fn proxy() -> Section {
    Section::Proxy
}

#[derive(Copy, Clone)]
pub enum Section {
    Proxy,
    Admin,
}

/// A utility for logging actions taken on behalf of a server task.
#[derive(Clone)]
pub struct Server {
    section: Section,
    name: &'static str,
    listen: SocketAddr,
    remote: Option<SocketAddr>,
}

/// A utility for logging actions taken on behalf of a client task.
#[derive(Clone)]
pub struct Client<C: fmt::Display, D: fmt::Display> {
    section: Section,
    client: C,
    dst: D,
    protocol: Option<::bind::Protocol>,
    remote: Option<SocketAddr>,
}

/// A utility for logging actions taken on behalf of a background task.
#[derive(Clone)]
pub struct Bg {
    section: Section,
    name: &'static str,
}

impl Section {
    pub fn bg(&self, name: &'static str) -> Bg {
        Bg {
            section: *self,
            name,
        }
    }

    pub fn server(&self, name: &'static str, listen: SocketAddr) -> Server {
        Server {
            section: *self,
            name,
            listen,
            remote: None,
        }
    }

    pub fn client<C: fmt::Display, D: fmt::Display>(&self, client: C, dst: D) -> Client<C, D> {
        Client {
            section: *self,
            client,
            dst,
            protocol: None,
            remote: None,
        }
    }
}

impl fmt::Display for Section {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            Section::Proxy => "proxy".fmt(f),
            Section::Admin => "admin".fmt(f),
        }
    }
}

pub type BgFuture<F> = ContextualFuture<Bg, F>;
pub type ClientExecutor<C, D> = ContextualExecutor<Client<C, D>>;
pub type ServerExecutor = ContextualExecutor<Server>;
pub type ServerFuture<F> = ContextualFuture<Server, F>;

impl Server {
    pub fn proxy(ctx: &::ctx::Proxy, listen: SocketAddr) -> Self {
        let name = if ctx.is_inbound() {
            "in"
        } else {
            "out"
        };
        Section::Proxy.server(name, listen)
    }

    pub fn with_remote(self, remote: SocketAddr) -> Self {
        Self {
            remote: Some(remote),
            .. self
        }
    }

    pub fn executor(self) -> ServerExecutor {
        context_executor(self)
    }

    pub fn future<F: Future>(self, f: F) -> ServerFuture<F> {
        context_future(self, f)
    }
}

impl fmt::Display for Server {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}={{server={} listen={}", self.section, self.name, self.listen)?;
        if let Some(remote) = self.remote {
            write!(f, " remote={}", remote)?;
        }
        write!(f, "}}")
    }
}

impl<D: fmt::Display> Client<&'static str, D> {
    pub fn proxy(ctx: &::ctx::Proxy, dst: D) -> Self {
        let name = if ctx.is_inbound() {
            "in"
        } else {
            "out"
        };
        Section::Proxy.client(name, dst)
    }
}

impl<C: fmt::Display, D: fmt::Display> Client<C, D> {
    pub fn with_protocol(self, p: ::bind::Protocol) -> Self {
        Self {
            protocol: Some(p),
            .. self
        }
    }

    pub fn with_remote(self, remote: SocketAddr) -> Self {
        Self {
            remote: Some(remote),
            .. self
        }
    }

    pub fn executor(self) -> ClientExecutor<C, D> {
        context_executor(self)
    }
}

impl<C: fmt::Display, D: fmt::Display> fmt::Display for Client<C, D> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}={{client={} dst={}", self.section, self.client, self.dst)?;
        if let Some(ref proto) = self.protocol {
            write!(f, " proto={:?}", proto)?;
        }
        if let Some(remote) = self.remote {
            write!(f, " remote={}", remote)?;
        }
        write!(f, "}}")
    }
}

impl Bg {
    pub fn future<F: Future>(self, f: F) -> BgFuture<F> {
        context_future(self, f)
    }
}

impl fmt::Display for Bg {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}={{bg={}}}", self.section, self.name)
    }
}
