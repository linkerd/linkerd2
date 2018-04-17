#![deny(missing_docs)]
use std::{fmt, u32};
use std::default::Default;
use std::time::Duration;

use super::{histogram, Counter};

/// The number of buckets in a  latency histogram.
pub const NUM_BUCKETS: usize = 26;

/// The maximum value (inclusive) for each latency bucket in
/// tenths of a millisecond.
pub const BUCKET_BOUNDS: [Latency; NUM_BUCKETS] = [
    // The controller telemetry server creates 5 sets of 5 linear buckets
    // each:
    // TODO: it would be nice if we didn't have to hard-code each
    //       individual bucket and could use Rust ranges or something.
    //       However, because we're using a raw fixed size array rather
    //       than a vector (as we don't ever expect to grow this array
    //       and thus don't _need_ a vector) we can't concatenate it
    //       from smaller arrays, making it difficult to construct
    //       programmatically...
    // in the controller:
    // prometheus.LinearBuckets(1, 1, 5),
    Latency(10),
    Latency(20),
    Latency(30),
    Latency(40),
    Latency(50),
    // prometheus.LinearBuckets(10, 10, 5),
    Latency(100),
    Latency(200),
    Latency(300),
    Latency(400),
    Latency(500),
    // prometheus.LinearBuckets(100, 100, 5),
    Latency(1_000),
    Latency(2_000),
    Latency(3_000),
    Latency(4_000),
    Latency(5_000),
    // prometheus.LinearBuckets(1000, 1000, 5),
    Latency(10_000),
    Latency(20_000),
    Latency(30_000),
    Latency(40_000),
    Latency(50_000),
    // prometheus.LinearBuckets(10000, 10000, 5),
    Latency(100_000),
    Latency(200_000),
    Latency(300_000),
    Latency(400_000),
    Latency(500_000),
    // Prometheus implicitly creates a max bucket for everything that
    // falls outside of the highest-valued bucket, but we need to
    // create it explicitly.
    Latency(u32::MAX),
];

/// A series of latency values and counts.
pub type Histogram = histogram::Histogram<'static, Latency>;

/// A latency in tenths of a millisecond.
#[derive(Debug, Default, Eq, PartialEq, Ord, PartialOrd, Copy, Clone, Hash)]
pub struct Latency(u32);


// ===== impl Histogram =====

impl Default for Histogram {
    fn default() -> Self {
        Histogram::new(&BUCKET_BOUNDS, Box::new([Counter::default(); NUM_BUCKETS]))
    }
}

impl<'a> histogram::Histogram<'a, Latency> {

    /// Return the sum value of this histogram in milliseconds.
    ///
    /// The sum is returned as a floating-point value, as it's
    /// internally recorded in tenths of milliseconds, which could
    /// represent a number of milliseconds with a fractional part.
    pub fn sum_in_ms(&self) -> f64 {
        self.sum.0 as f64 / MS_TO_TENTHS_OF_MS as f64
    }
}

// ===== impl Latency =====

const SEC_TO_MS: u32 = 1_000;
const SEC_TO_TENTHS_OF_A_MS: u32 = SEC_TO_MS * 10;
const TENTHS_OF_MS_TO_NS: u32 =  MS_TO_NS / 10;
const MS_TO_TENTHS_OF_MS: u32 = 10;
/// Conversion ratio from milliseconds to nanoseconds.
pub const MS_TO_NS: u32 = 1_000_000;

impl From<Duration> for Latency {
    fn from(dur: Duration) -> Self {
        let secs = dur.as_secs();
        // checked conversion from u64 -> u32.
        let secs =
            if secs >= u64::from(u32::MAX) {
                None
            } else {
                Some(secs as u32)
            };
        // represent the duration as tenths of a ms.
        let tenths_of_ms = {
            let t = secs.and_then(|as_secs|
                // convert the number of seconds to tenths of a ms, or
                // None on overflow.
                as_secs.checked_mul(SEC_TO_TENTHS_OF_A_MS)
            );
            let t = t.and_then(|as_tenths_ms| {
                // convert the subsecond part of the duration (in ns) to
                // tenths of a millisecond.
                let subsec_tenths_ms = dur.subsec_nanos() / TENTHS_OF_MS_TO_NS;
                as_tenths_ms.checked_add(subsec_tenths_ms)
            });
            t.unwrap_or_else(|| {
                debug!(
                    "{:?} too large to represent as tenths of a \
                     millisecond!",
                     dur
                );
                u32::MAX
            })
        };
        Latency(tenths_of_ms)
    }

}

impl From<u32> for Latency {
    #[inline]
    fn from(value: u32) -> Self {
        Latency(value)
    }
}

impl Into<u32> for Latency {
    fn into(self) -> u32 {
        self.0
    }
}

impl Into<u64> for Latency {
    fn into(self) -> u64 {
        self.0 as u64
    }
}

impl fmt::Display for Latency {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        if self.0 == u32::MAX {
            // Prometheus requires that text representations of numbers be in
            // a format understandable by Go's strconv package. In particular,
            // `-Inf`, `+Inf`, and `Nan` are used as the textual
            // representations of floating point special values.
            //
            // We're representing latency buckets with u32s rather than floats,
            // so we won't encounter these special values, but we want to treat
            // the u32::MAX upper bound as the infinity bucket, so special case
            // the formatting for u32::MAX.
            write!(f, "+Inf")
        } else {
            // NOTE: if bucket values are changed so that they're no longer
            //       evenly divisible by ten, we may want to ensure that there's
            //       a reasonable cap on precision here.
            write!(f, "{}", self.0 / MS_TO_TENTHS_OF_MS)
        }
    }
}

impl histogram::FormatSum for Latency {
    fn format_sum<'a>(hist: &histogram::Histogram<'a, Latency>) -> f64 {
        hist.sum_in_ms()
    }
}
