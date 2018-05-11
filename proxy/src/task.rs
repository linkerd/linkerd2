//! Task execution utilities.
use futures::future::{
    Future,
    ExecuteError,
    ExecuteErrorKind,
    Executor,
};
use rand;
use tokio::executor::{
    DefaultExecutor,
    Executor as TokioExecutor,
    SpawnError,
};

use std::error::Error;
use std::{fmt, io};


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

/// Like a `SpawnError` or `ExecuteError`, but with an implementation
/// of `std::error::Error`.
#[derive(Debug, Clone)]
pub enum TaskError {
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

// ===== impl LazyRng =====

impl rand::Rng for LazyRng {
    fn next_u32(&mut self) -> u32 {
        rand::thread_rng().next_u32()
    }

    fn next_u64(&mut self) -> u64 {
        rand::thread_rng().next_u64()
    }

    #[inline]
    fn fill_bytes(&mut self, bytes: &mut [u8]) {
        rand::thread_rng().fill_bytes(bytes)
    }
}

// ===== impl TaskError =====

impl TaskError {

    /// Wrap a `SpawnError` or `ExecuteError` in an `io::Error`.
    ///
    /// The returned `io::Error` will have `ErrorKind::Other`. Wrapping
    /// the error in `TaskError` is necessary as the type passed to
    /// `io::Error::new` must implement `std::error::Error`.
    pub fn into_io<I: Into<TaskError>>(inner: I) -> io::Error {
        io::Error::new(io::ErrorKind::Other, inner.into())
    }
}

impl From<SpawnError> for TaskError {
    fn from(spawn_error: SpawnError) -> Self {
        if spawn_error.is_shutdown() {
            TaskError::Shutdown
        } else if spawn_error.is_at_capacity() {
            TaskError::NoCapacity
        } else {
            warn!(
                "TaskError::from: unknown SpawnError '{:?}'\n\
                 This indicates a change in Tokio's API surface that should\n\
                 be handled.",
                 spawn_error,
            );
            TaskError::Unknown
        }
    }
}

impl<F> From<ExecuteError<F>> for TaskError {
    fn from(exec_error: ExecuteError<F>) -> Self {
        match exec_error.kind() {
            ExecuteErrorKind::Shutdown => TaskError::Shutdown,
            ExecuteErrorKind::NoCapacity => TaskError::NoCapacity,
            _ => {
                warn!(
                    "TaskError::from: unknown ExecuteError '{:?}'\n\
                    This indicates a change in the futures-rs API surface\n\
                    that should be handled.",
                    exec_error,
                );
                TaskError::Unknown
            }
        }
    }
}
impl fmt::Display for  TaskError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        self.description().fmt(f)
    }
}

impl Error for TaskError {
    fn description(&self) -> &str {
        match *self {
            TaskError::Shutdown => "executor has shut down",
            TaskError::NoCapacity => "executor has no more capacity",
            TaskError::Unknown => "unknown error executing future",
        }
    }
}
