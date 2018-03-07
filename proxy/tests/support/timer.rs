use support::*;

use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, Instant};
use std::ops::AddAssign;

use self::futures::sync::oneshot;
use self::indexmap::IndexMap;
use self::tokio_core::reactor;

use self::conduit_proxy::time::Timer;

/// A mock timer.
#[derive(Clone, Debug)]
pub struct MockTimer {
    inner: Arc<Inner>,
}

/// A handle for controlling the mock timer.
#[derive(Clone, Debug)]
pub struct Control {
    inner: Arc<Inner>,
}

type Timeouts = IndexMap<
    Instant,
    Vec<oneshot::Sender<()>>,
>;

#[derive(Debug)]
struct Inner {
    now: RwLock<Instant>,
    timeouts: Mutex<Timeouts>,
}

/// Construct a new mock timer.
///
/// Returns the `MockTimer` handle, which implements `Timer`, and the
/// `Control` handle, which may be used by the test case to control
/// the mock timer.
pub fn mock() -> (MockTimer, Control) {
    let inner = Arc::new(Inner::new());
    let timer = MockTimer {
        inner: Arc::clone(&inner),
    };
    let control = Control {
        inner: Arc::clone(&inner),
    };

    (timer, control)
}

// ===== impl MockTimer =====

impl Timer for MockTimer  {
    /// The type of the future returned by `sleep`.
    type Sleep = oneshot::Receiver<()>;
    /// Error type for the `Sleep` future.
    type Error = oneshot::Canceled;

    /// Returns a future that completes after the given duration.
    fn sleep(&self, duration: Duration) -> Self::Sleep {
        let until = self.now() + duration;
        let (tx, rx) = oneshot::channel::<()>();

        self.inner.timeouts.lock()
            .unwrap()
            .entry(until)
            .or_insert_with(Vec::new)
            .push(tx);
        rx
    }

    /// Returns the current time.
    ///
    /// This takes `&self` primarily for the mock timer implementation.
    fn now(&self) -> Instant {
        self.inner.now()
    }

    fn with_handle(self, _handle: &reactor::Handle) -> Self {
        self
    }
}

// ===== impl Inner =====

impl Inner {
    fn new() -> Self {
        Inner {
            now: RwLock::new(Instant::now()),
            timeouts: Mutex::new(Timeouts::new())
        }
    }

    fn now(&self) -> Instant {
        self.now.read().unwrap().clone()
    }

    fn set_time(&self, to: Instant) {
        let mut now = self.now.write()
            .unwrap();
        let mut timeouts = self.timeouts.lock()
            .unwrap();

        // Advance the current time to the given instant.
        *now = to;

        // Get all the keys whose durations have timed out.
        let timed_out_keys: Vec<Instant> = timeouts
            .keys()
            .take_while(|&t| t <= &to)
            // XXX this is pretty inefficient, but it should only happen in
            // unit tests, so it doesn't really need to be optimized...
            .cloned()
            .collect();

        for t in timed_out_keys {
            timeouts.remove(&t)
                .expect("map should have values for all keys in key set")
                .into_iter()
                // Don't notify timeouts whose receivers that have been dropped.
                .filter(|tx| !tx.is_canceled())
                // If the reciever hasn't been dropped, send () to finish the
                // timeout future.
                .for_each(|tx|
                    tx.send(())
                        .expect("send should succeed if rx was not canceled")
                );
        }

    }
}

// ===== impl Control =====

impl Control {

    pub fn set_time(&mut self, to: Instant) {
        self.inner.set_time(to)
    }

    pub fn advance_by(&mut self, amount: Duration) {
        let now = self.inner.now();
        self.set_time(now + amount)
    }
}

impl AddAssign<Duration> for Control {

    fn add_assign(&mut self, amount: Duration) {
        self.advance_by(amount)
    }

}
