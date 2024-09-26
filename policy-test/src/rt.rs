//! HTTP runtime components for Linkerd.

use hyper::rt::Executor;
use std::future::Future;

#[derive(Clone, Debug, Default)]
pub struct TokioExecutor;

impl<F> Executor<F> for TokioExecutor
where
    F: Future + Send + 'static,
    F::Output: Send + 'static,
{
    #[inline]
    fn execute(&self, f: F) {
        tokio::spawn(f);
    }
}
