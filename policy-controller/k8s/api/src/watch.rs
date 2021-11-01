use futures::prelude::*;
pub use kube::runtime::watcher::{Event, Result};
use std::pin::Pin;
use tokio::time;
use tracing::{info, Instrument};

/// Wraps an event stream that never terminates.
pub struct Watch<T> {
    initialized: bool,
    span: tracing::Span,
    rx: Pin<Box<dyn Stream<Item = Result<Event<T>>> + Send + 'static>>,
}

// === impl Watch ===

impl<T, W> From<W> for Watch<T>
where
    W: Stream<Item = Result<Event<T>>> + Send + 'static,
{
    fn from(watch: W) -> Self {
        Self::new(watch.boxed())
    }
}

impl<T> Watch<T> {
    pub fn new(rx: Pin<Box<dyn Stream<Item = Result<Event<T>>> + Send + 'static>>) -> Watch<T> {
        Self {
            rx,
            initialized: false,
            span: tracing::Span::current(),
        }
    }

    pub fn instrument(mut self, span: tracing::Span) -> Self {
        self.span = span;
        self
    }

    pub fn is_initialized(&self) -> bool {
        self.initialized
    }

    /// Receive the next event in the stream.
    ///
    /// If the stream fails, log the error and sleep for 1s before polling for a reset event.
    pub async fn recv(&mut self) -> Event<T> {
        loop {
            let ev = self
                .rx
                .next()
                .instrument(self.span.clone())
                .await
                .expect("stream must not terminate");

            match ev {
                Ok(ev) => {
                    self.initialized = true;
                    return ev;
                }
                Err(error) => {
                    info!(parent: &self.span, %error, "Failed");

                    // TODO(ver) this backoff may not be fully honored if this call to `recv` is
                    // canceled. Instead, we should track the backoff on the `Watch` and poll it
                    // before the inner stream. We probably need to use pin-project-lite for this,
                    // though.
                    time::sleep(time::Duration::from_secs(1)).await;
                    info!(parent: &self.span, "Restarting");
                }
            }
        }
    }
}
