#![deny(missing_docs)]
use control::pb::telemetry::LatencyBucket;

use std::{ops, slice, u32};
use std::default::Default;
use std::time::Duration;

/// The number of buckets in a  latency histogram.
pub const NUM_BUCKETS: usize = 26;

/// A series of latency values and counts.
#[derive(Debug)]
pub struct Histogram([Bucket<Latency>; NUM_BUCKETS]);

/// A latency in tenths of a millisecond.
#[derive(Debug, Default, Eq, PartialEq, Ord, PartialOrd, Copy, Clone, Hash)]
pub struct Latency(u32);

/// A bucket in a latency histogram.
///
/// # Type Parameters:
/// - `T`: the type of observations counted by this bucket.
/// - `C`: the type used for counting observed values.
#[derive(Debug, Clone)]
pub struct Bucket<T, C = u32> {

    /// The inclusive upper bound on the range of values in this bucket.
    max: T,

    /// Count of observations falling into this bucket.
    count: C,

}


impl<T> Bucket<T> {

    // Construct a new `Bucket` with the inclusive upper bound of `max`.
    fn new<I: Into<T>>(max: I) -> Self {
        Bucket {
            max: max.into(),
            count: 0
        }
    }

}

impl<T> Bucket<T>
where
    T: PartialOrd
{

    fn contains(&self, t: &T) -> bool {
        t <= &self.max
    }

    fn observe(&mut self, t: &T) -> bool {
        if self.contains(t) {
            // silently truncate if the count is over u32::MAX.
            self.count = self.count.saturating_add(1);
            true
        } else {
            false
        }

    }
}



// ===== impl Histogram =====

impl Histogram {

    /// Observe a measurement
    pub fn observe<I>(&mut self, measurement: I)
    where
        I: Into<Latency>,
    {
        let measurement = measurement.into();
        for ref mut bucket in self {
            if bucket.observe(&measurement) {
                return;
            }
        }
    }

    /// Construct a new, empty `Histogram`.
    ///
    /// The buckets in this `Histogram` should mimic the Prometheus buckets
    /// created by the Conduit controller's telemetry server, but with max
    /// values one order of magnitude higher. This is because we're recording
    /// latencies in tenths of a millisecond, but truncating these observations
    /// to millisecond resolution.
    pub fn new() -> Self {
        // The controller telemetry server creates 5 sets of 5 linear buckets
        // each:
        Histogram([
            // TODO: it would be nice if we didn't have to hard-code each
            //       individual bucket and could use Rust ranges or something.
            //       However, because we're using a raw fixed size array rather
            //       than a vector (as we don't ever expect to grow this array
            //       and thus don't _need_ a vector) we can't concatenate it
            //       from smaller arrays, making it difficult to construct
            //       programmatically...
            // in the controller:
            // prometheus.LinearBuckets(1, 1, 5),
            Bucket::new(10),
            Bucket::new(20),
            Bucket::new(30),
            Bucket::new(40),
            Bucket::new(50),
            // prometheus.LinearBuckets(10, 10, 5),
            Bucket::new(100),
            Bucket::new(200),
            Bucket::new(300),
            Bucket::new(400),
            Bucket::new(500),
            // prometheus.LinearBuckets(100, 100, 5),
            Bucket::new(1_000),
            Bucket::new(2_000),
            Bucket::new(3_000),
            Bucket::new(4_000),
            Bucket::new(5_000),
            // prometheus.LinearBuckets(1000, 1000, 5),
            Bucket::new(10_000),
            Bucket::new(20_000),
            Bucket::new(30_000),
            Bucket::new(40_000),
            Bucket::new(50_000),
            // prometheus.LinearBuckets(10000, 10000, 5),
            Bucket::new(100_000),
            Bucket::new(200_000),
            Bucket::new(300_000),
            Bucket::new(400_000),
            Bucket::new(500_000),
            // Prometheus implicitly creates a max bucket for everything that
            // falls outside of the highest-valued bucket, but we need to
            // create it explicitly.
            Bucket::new(u32::MAX),
        ])
    }

}

impl<I> ops::AddAssign<I> for Histogram
where
    I: Into<Latency>
{
    #[inline]
    fn add_assign(&mut self, measurement: I) {
        self.observe(measurement)
    }

}


impl<'a> IntoIterator for &'a Histogram {
    type Item = &'a Bucket<Latency>;
    type IntoIter = slice::Iter<'a, Bucket<Latency>>;

    fn into_iter(self) -> Self::IntoIter {
        self.0.iter()
    }

}

impl<'a> IntoIterator for &'a mut Histogram {
    type Item = &'a mut Bucket<Latency>;
    type IntoIter = slice::IterMut<'a, Bucket<Latency>>;

    fn into_iter(self) -> Self::IntoIter {
        self.0.iter_mut()
    }

}

impl Default for Histogram {
    #[inline]
    fn default() -> Self {
        Self::new()
    }
}

impl<'a> Into<Vec<LatencyBucket>> for &'a Histogram {
    fn into(self) -> Vec<LatencyBucket> {
        self.0.into_iter()
            .map(LatencyBucket::from)
            .collect()
    }
}

impl Into<Vec<LatencyBucket>> for Histogram {
    fn into(self) -> Vec<LatencyBucket> {
        self.0.into_iter()
            .map(LatencyBucket::from)
            .collect()
    }
}

// ===== impl Latency =====

const SEC_TO_MS: u32 = 1_000;
const SEC_TO_TENTHS_OF_A_MS: u32 = SEC_TO_MS * 10;
const TENTHS_OF_MS_TO_NS: u32 =  MS_TO_NS / 10;
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

// ===== impl LatencyBucket =====

impl<'a> From<&'a Bucket<Latency>> for LatencyBucket {
    fn from(bucket: &'a Bucket<Latency>) -> Self {
        LatencyBucket {
            max_value: bucket.max.into(),
            count: bucket.count,
        }
    }
}