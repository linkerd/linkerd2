use std::fmt;

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

impl fmt::Display for Gauge {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        self.0.fmt(f)
    }
}
