use std::{fmt, ops};
use std::num::Wrapping;

/// A Prometheus counter is represented by a `Wrapping` unsigned 64-bit int.
///
/// Counters always explicitly wrap on overflows rather than panicking in
/// debug builds. Prometheus' [`rate()`] and [`irate()`] queries handle breaks
/// in monotonicity gracefully  (see also [`resets()`]), so wrapping is less
/// problematic than panicking in this case.
///
/// Note, however, that Prometheus represents counters using 64-bit
/// floating-point numbers. The correct semantics are to ensure the counter
/// always gets reset to zero after Prometheus reads it, before it would ever
/// overflow a 52-bit `f64` mantissa.
///
/// [`rate()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#rate()
/// [`irate()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#irate()
/// [`resets()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#resets
///
// TODO: Implement Prometheus reset semantics correctly, taking into
//       consideration that Prometheus models counters as `f64` and so
//       there are only 52 significant bits.
#[derive(Copy, Clone, Debug, Default, Eq, PartialEq)]
pub struct Counter(Wrapping<u64>);

// ===== impl Counter =====

impl Counter {
    /// Increment the counter by one.
    ///
    /// This function wraps on overflows.
    pub fn incr(&mut self) {
        (*self).0 += Wrapping(1);
    }
}

impl Into<u64> for Counter {
    fn into(self) -> u64 {
        (self.0).0
    }
}

impl ops::Add for Counter {
    type Output = Self;
    fn add(self, Counter(rhs): Self) -> Self::Output {
        Counter(self.0 + rhs)
    }
}

impl ops::AddAssign<u64> for Counter {
    fn add_assign(&mut self, rhs: u64) {
        (*self).0 += Wrapping(rhs)
    }
}

impl fmt::Display for Counter {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        self.0.fmt(f)
    }
}
