use futures::prelude::*;
use std::pin::Pin;
use tokio::time;
use tracing::info;

pub use kube_runtime::watcher::{Event, Result};

/// Wraps an event stream that never terminates.
pub struct Watch<T> {
    ready: bool,
    rx: Pin<Box<dyn Stream<Item = Result<Event<T>>> + Send + 'static>>,
}

// === impl Watch ===

impl<T, W> From<W> for Watch<T>
where
    W: Stream<Item = Result<Event<T>>> + Send + 'static,
{
    fn from(watch: W) -> Self {
        Watch {
            ready: false,
            rx: watch.boxed(),
        }
    }
}

impl<T> Watch<T> {
    pub fn ready(&self) -> bool {
        self.ready
    }

    /// Receive the next event in the stream.
    ///
    /// If the stream fails, log the error and sleep for 1s before polling for a reset event.
    pub async fn recv(&mut self) -> Event<T> {
        loop {
            match self
                .rx
                .next()
                .await
                .expect("watch stream must not terminate")
            {
                Ok(ev) => {
                    self.ready = true;
                    return ev;
                }
                Err(error) => {
                    self.ready = false;
                    info!(%error, "Disconnected");
                    time::sleep(time::Duration::from_secs(1)).await;
                }
            }
        }
    }
}
