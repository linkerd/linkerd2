use std::cell::RefCell;
use std::env;
use std::io::Write;
use std::fmt;
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
                    "{} {} {}{}",
                    level,
                    record.target(),
                    Context(&ctxt.borrow()),
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
pub fn context_future<T: fmt::Display, F>(context: T, future: F) -> ContextualFuture<T, F> {
    ContextualFuture {
        context,
        future,
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
pub struct ContextualFuture<T, F> {
    context: T,
    future: F,
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
        let fut = &mut self.future;
        context(ctxt, || fut.poll())
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
                let future = err.into_future();
                Err(ExecuteError::new(kind, future.future))
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
            item.fmt(f)?;
            f.write_str(", ")?;
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

