use std::time::Duration;

use super::histogram::{Bounds, Bucket, Histogram};

/// The maximum value (inclusive) for each latency bucket in
/// milliseconds.
pub const BOUNDS: &Bounds = &Bounds(&[
    Bucket::Le(1),
    Bucket::Le(2),
    Bucket::Le(3),
    Bucket::Le(4),
    Bucket::Le(5),
    Bucket::Le(10),
    Bucket::Le(20),
    Bucket::Le(30),
    Bucket::Le(40),
    Bucket::Le(50),
    Bucket::Le(100),
    Bucket::Le(200),
    Bucket::Le(300),
    Bucket::Le(400),
    Bucket::Le(500),
    Bucket::Le(1_000),
    Bucket::Le(2_000),
    Bucket::Le(3_000),
    Bucket::Le(4_000),
    Bucket::Le(5_000),
    Bucket::Le(10_000),
    Bucket::Le(20_000),
    Bucket::Le(30_000),
    Bucket::Le(40_000),
    Bucket::Le(50_000),
    // A final upper bound.
    Bucket::Inf,
]);

/// A duration in milliseconds.
#[derive(Debug, Default, Clone)]
pub struct Ms(Duration);

impl Into<u64> for Ms {
    fn into(self) -> u64 {
        self.0.as_secs().saturating_mul(1_000)
            .saturating_add(u64::from(self.0.subsec_nanos()) / 1_000_000)
    }
}

impl From<Duration> for Ms {
    fn from(d: Duration) -> Self {
        Ms(d)
    }
}

impl Default for Histogram<Ms> {
    fn default() -> Self {
        Histogram::new(BOUNDS)
    }
}
