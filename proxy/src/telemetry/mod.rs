//! Sensors and reports telemetry from the proxy.

use std::sync::Arc;
use std::time::Duration;

use futures_mpsc_lossy;

use ctx;
use time::Timer;

mod control;
pub mod event;
mod metrics;
pub mod sensor;
pub mod tap;

pub use self::control::{Control, MakeControl};
pub use self::event::Event;
pub use self::sensor::Sensors;

/// Creates proxy-specific runtime telemetry.
///
/// [`Sensors`] hide the details of how telemetry is recorded, but expose proxy utilties
/// that support telemetry.
///
/// [`Control`] drives processing of all telemetry events for tapping as well as metrics
/// reporting.
///
/// # Arguments
/// - `capacity`: the number of events to aggregate.
/// - `flush_interval`: the length of time after which a metrics report should
///    be sent, regardless of how many events have been aggregated.
/// - `timer`: a [`Timer`] that provides timestamps for events.
///
/// [`Sensors`]: struct.Sensors.html
/// [`Control`]: struct.Control.html
// TODO: use the `timer` for the metrics flush timeout as well.
pub fn new<T: Timer>(
    process: &Arc<ctx::Process>,
    capacity: usize,
    flush_interval: Duration,
    timer: &T,
) -> (Sensors<T>, MakeControl) {
    let (tx, rx) = futures_mpsc_lossy::channel(capacity);
    let s = Sensors::new(tx, timer);
    let c = MakeControl::new(rx, flush_interval, process);
    (s, c)
}
