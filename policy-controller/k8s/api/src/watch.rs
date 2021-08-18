use futures::prelude::*;
use std::pin::Pin;
use tokio::time;
use tracing::{info, Instrument};

pub use kube_runtime::watcher::{Event, Result};

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
                .expect(&*format!(
                    "{} stream must not terminate",
                    std::any::type_name::<T>()
                ));

            match ev {
                Ok(ev) => {
                    self.initialized = true;
                    return ev;
                }
                Err(error) => {
                    info!(parent: &self.span, %error, "Failed");
                    time::sleep(time::Duration::from_secs(1)).await;
                    info!(parent: &self.span, "Restarting");
                }
            }
        }
    }
}
