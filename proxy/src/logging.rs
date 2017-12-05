use std::cell::RefCell;
use std::env;
use std::fmt;
use std::rc::Rc;

use chrono::Utc;
use futures::{Future, Poll};
use futures::future::{Executor, ExecuteError};
use env_logger::LogBuilder;
use log::LogLevel;

const ENV_LOG: &str = "CONDUIT_PROXY_LOG";

thread_local! {
    static CONTEXT: RefCell<Vec<*const fmt::Debug>> = RefCell::new(Vec::new());
}

pub fn init() {
    LogBuilder::new()
        .format(|record| {
            CONTEXT.with(|ctxt| {
                let level = match record.level() {
                    LogLevel::Trace => "TRCE",
                    LogLevel::Debug => "DBUG",
                    LogLevel::Info => "INFO",
                    LogLevel::Warn => "WARN",
                    LogLevel::Error => "ERR!",
                };
                format!(
                    "{} {} {} {:?}{}",
                    Utc::now().format("%s%.6f"),
                    level,
                    record.target(),
                    Context(&ctxt.borrow()),
                    record.args()
                )
            })
        })
        .parse(&env::var(ENV_LOG).unwrap_or_default())
        .init()
        .expect("logger");

}

/// Execute a closure with a `Debug` item attached to allow log messages.
pub fn context<T, F, U>(context: &T, mut closure: F) -> U
where
    T: ::std::fmt::Debug + 'static,
    F: FnMut() -> U,
{
    // This is a raw pointer because of lifetime conflicts that require
    // the thread local to have a static lifetime.
    //
    // We don't want to require a static lifetime, and in fact,
    // only use the reference within this closure, so converting
    // to a raw pointer is safe.
    let context = context as *const fmt::Debug;
    CONTEXT.with(|ctxt| {
        ctxt.borrow_mut().push(context);
        let ret = closure();
        ctxt.borrow_mut().pop();
        ret
    })
}

/// Wrap a `Future` with a `Debug` value that will be inserted into all logs
/// created by this Future.
pub fn context_future<T, F>(context: T, future: F) -> ContextualFuture<T, F> {
    ContextualFuture {
        context,
        future,
    }
}

/// Wrap an `Executor` to spawn futures that have a reference to the `Debug`
/// value, inserting it into all logs created by this future.
pub fn context_executor<T, E>(context: T, executor: E) -> ContextualExecutor<T, E> {
    ContextualExecutor {
        context: Rc::new(context),
        executor,
    }
}

#[derive(Debug)]
pub struct ContextualFuture<T, F> {
    context: T,
    future: F,
}

impl<T, F> Future for ContextualFuture<T, F>
where
    T: ::std::fmt::Debug + 'static,
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

#[derive(Clone, Debug)]
pub struct ContextualExecutor<T, E> {
    context: Rc<T>,
    executor: E,
}

impl<T, E, F> Executor<F> for ContextualExecutor<T, E>
where
    T: ::std::fmt::Debug + 'static,
    E: Executor<ContextualFuture<Rc<T>, F>>,
    F: Future<Item=(), Error=()>,
{
    fn execute(&self, future: F) -> Result<(), ExecuteError<F>> {
        let fut = context_future(self.context.clone(), future);
        match self.executor.execute(fut) {
            Ok(()) => Ok(()),
            Err(err) => {
                let kind = err.kind();
                let future = err.into_future();
                Err(ExecuteError::new(kind, future.future))
            }
        }
    }
}

struct Context<'a>(&'a [*const fmt::Debug]);

impl<'a> fmt::Debug for Context<'a> {
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
