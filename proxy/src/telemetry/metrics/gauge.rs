use std::fmt::{self, Display};

use super::FmtMetric;

/// An instaneous metric value.
#[derive(Copy, Clone, Debug, Default, Eq, PartialEq)]
pub struct Gauge(u64);

impl Gauge {
    /// Increment the gauge by one.
    pub fn incr(&mut self) {
        if let Some(new_value) = self.0.checked_add(1) {
            (*self).0 = new_value;
        } else {
            warn!("Gauge overflow");
        }
    }

    /// Decrement the gauge by one.
    pub fn decr(&mut self) {
        if let Some(new_value) = self.0.checked_sub(1) {
            (*self).0 = new_value;
        } else {
            warn!("Gauge underflow");
        }
    }
}

impl From<u64> for Gauge {
    fn from(n: u64) -> Self {
        Gauge(n)
    }
}

impl Into<u64> for Gauge {
    fn into(self) -> u64 {
        self.0
    }
}

impl FmtMetric for Gauge {
    fn fmt_metric<N: Display>(&self, f: &mut fmt::Formatter, name: N) -> fmt::Result {
        writeln!(f, "{} {}", name, self.0)
    }

    fn fmt_metric_labeled<N, L>(&self, f: &mut fmt::Formatter, name: N, labels: L) -> fmt::Result
    where
        N: Display,
        L: Display,
    {
        writeln!(f, "{name}{{{labels}}} {value}",
            name = name,
            labels = labels,
            value = self.0,
        )
    }
}
