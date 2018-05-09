//! Task execution utilities.
use futures::future::{ExecuteError, ExecuteErrorKind};
use tokio::executor::SpawnError;

use std::error::Error;
use std::{fmt, io};

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


//===== impl TaskError =====

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
