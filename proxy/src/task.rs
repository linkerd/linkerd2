//! Task execution utilities.
use futures::future::{
    Future,
    ExecuteError,
    ExecuteErrorKind,
    Executor,
};
use tokio::{
    executor::{
        DefaultExecutor,
        Executor as TokioExecutor,
        SpawnError,
    },
    runtime::{self as thread_pool, current_thread},
};

use std::{
    error::Error as StdError,
    fmt,
    io,
};

/// An empty type which implements `Executor` by lazily  calling
/// `tokio::executor::DefaultExecutor::current().execute(...)`.
///
/// This can be used when we would simply like to call `tokio::spawn` rather
/// than explicitly using a particular executor, but need an `Executor` for
/// a generic type or to pass to a function which expects one.
///
/// Note that this uses `DefaultExecutor` rather than `tokio::spawn`, as we
/// would prefer for our `Executor` implementation to pass errors rather than
/// panicking (as `tokio::spawn` does).
#[derive(Copy, Clone, Debug, Default)]
pub struct LazyExecutor;

/// Indicates which Tokio `Runtime` should be used for the main proxy.
///
/// This is either a `tokio::runtime::current_thread::Runtime`, or a
/// `tokio::runtime::Runtime` (thread pool). This type simply allows
/// both runtimes to present a unified interface, so that they can be
/// used to construct a `Main`.
///
/// This allows the runtime used for the proxy to be customized based
/// on the application: for a sidecar proxy, we use the current thread
/// runtime, but for an ingress proxy, we would prefer the thread pool.
pub enum MainRuntime {
    CurrentThread(current_thread::Runtime),
    ThreadPool(thread_pool::Runtime),
}

/// Like a `SpawnError` or `ExecuteError`, but with an implementation
/// of `std::error::Error`.
#[derive(Debug, Clone)]
pub enum Error {
    /// The executor has shut down and will no longer spawn new tasks.
    Shutdown,
    /// The executor had no capacity to run more futures.
    NoCapacity,
    /// An unknown error occurred.
    ///
    /// This indicates that `tokio` or `futures-rs` has
    /// added additional error types that we are not aware of.
    Unknown,
}

// ===== impl LazyExecutor =====;

impl TokioExecutor for LazyExecutor {
    fn spawn(
        &mut self,
        future: Box<Future<Item = (), Error = ()> + 'static + Send>
    ) -> Result<(), SpawnError>
    {
        DefaultExecutor::current().spawn(future)
    }

    fn status(&self) -> Result<(), SpawnError> {
        DefaultExecutor::current().status()
    }
}

impl<F> Executor<F> for LazyExecutor
where
    F: Future<Item = (), Error = ()> + 'static + Send,
{
    fn execute(&self, future: F) -> Result<(), ExecuteError<F>> {
        let mut executor = DefaultExecutor::current();
        // Check the status of the executor first, so that we can return the
        // future in the `ExecuteError`. If we just called `spawn` and
        // `map_err`ed the error into an `ExecuteError`, we'd have to move the
        // future into the closure, but it was already moved into `spawn`.
        if let Err(e) = executor.status() {
            if e.is_at_capacity() {
                return Err(ExecuteError::new(ExecuteErrorKind::NoCapacity, future));
            } else if e.is_shutdown() {
                return Err(ExecuteError::new(ExecuteErrorKind::Shutdown, future));
            } else {
                panic!("unexpected `SpawnError`: {:?}", e);
            }
        };
        executor.spawn(Box::new(future))
            .expect("spawn() errored but status() was Ok");
        Ok(())
    }
}

// ===== impl MainRuntime =====

impl MainRuntime {
    /// Spawn a task on this runtime.
    pub fn spawn<F>(&mut self, future: F) -> &mut Self
    where
        F: Future<Item = (), Error = ()> + Send + 'static,
    {
        match *self {
            MainRuntime::CurrentThread(ref mut rt) => { rt.spawn(future); }
            MainRuntime::ThreadPool(ref mut rt) => {  rt.spawn(future); }
        };
        self
    }

    /// Runs `self` until `shutdown_signal` completes.
    pub fn run_until<F>(self, shutdown_signal: F)  -> Result<(), ()>
    where
        F: Future<Item = (), Error = ()> + Send + 'static,
    {
        match self {
            MainRuntime::CurrentThread(mut rt) =>
                rt.block_on(shutdown_signal),
            MainRuntime::ThreadPool(rt) =>
                shutdown_signal
                    .and_then(move |()| rt.shutdown_now())
                    .wait(),
        }
    }
}

impl From<current_thread::Runtime> for MainRuntime {
    fn from(rt: current_thread::Runtime) -> Self {
        debug!("creating single-threaded proxy");
        MainRuntime::CurrentThread(rt)
    }
}

impl From<thread_pool::Runtime> for MainRuntime {
    fn from(rt: thread_pool::Runtime) -> Self {
        debug!("creating proxy with threadpool");
        MainRuntime::ThreadPool(rt)
    }
}

// ===== impl Error =====

impl Error {

    /// Wrap a `SpawnError` or `ExecuteError` in an `io::Error`.
    ///
    /// The returned `io::Error` will have `ErrorKind::Other`. Wrapping
    /// the error in `Error` is necessary as the type passed to
    /// `io::Error::new` must implement `std::error::Error`.
    pub fn into_io<I: Into<Self>>(inner: I) -> io::Error {
        io::Error::new(io::ErrorKind::Other, inner.into())
    }
}

impl From<SpawnError> for Error {
    fn from(spawn_error: SpawnError) -> Self {
        if spawn_error.is_shutdown() {
            Error::Shutdown
        } else if spawn_error.is_at_capacity() {
            Error::NoCapacity
        } else {
            warn!(
                "Error::from: unknown SpawnError '{:?}'\n\
                 This indicates a change in Tokio's API surface that should\n\
                 be handled.",
                 spawn_error,
            );
            Error::Unknown
        }
    }
}

impl<F> From<ExecuteError<F>> for Error {
    fn from(exec_error: ExecuteError<F>) -> Self {
        match exec_error.kind() {
            ExecuteErrorKind::Shutdown => Error::Shutdown,
            ExecuteErrorKind::NoCapacity => Error::NoCapacity,
            _ => {
                warn!(
                    "Error::from: unknown ExecuteError '{:?}'\n\
                    This indicates a change in the futures-rs API surface\n\
                    that should be handled.",
                    exec_error,
                );
                Error::Unknown
            }
        }
    }
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        self.description().fmt(f)
    }
}

impl StdError for Error {
    fn description(&self) -> &str {
        match *self {
            Error::Shutdown => "executor has shut down",
            Error::NoCapacity => "executor has no more capacity",
            Error::Unknown => "unknown error executing future",
        }
    }
}
