#![deny(missing_docs)]
use control::pb::telemetry::{
    Latency as LatencyProto,
};

use std::{ops, slice, u32};
use std::default::Default;
use std::time::Duration;

/// A series of latency values and counts.
#[derive(Debug)]
pub struct Latencies(Buckets<Latency, Buckets<Latency>>);

/// A latency in tenths of a millisecond.
#[derive(Debug, Default, Eq, PartialEq, Ord, PartialOrd, Copy, Clone, Hash)]
pub struct Latency(u32);

/// Generalization of a histogram bucket counting observations of type `T`.
trait Bucket<T: PartialOrd> {

    /// Returns `true` if a given `observation` falls into this `Bucket`.
    fn contains(&self, observation: &T) -> bool;

    /// Observe a value, incrementing this bucket if it `contains` that value.
    ///
    /// # Returns
    /// - `true` if this `Bucket`'s count was incremented;
    /// - `false` otherwise.
    fn add(&mut self, observation: &T) -> bool;

}

/// A "leaf" bucket containing a single counter.
///
/// # Type Parameters:
/// - `T`: the type of observations counted by this bucket.
/// - `C`: the type used for counting observed values.
#[derive(Debug, Clone)]
struct LeafBucket<T, C = u32> {

    /// The upper bound of the range of values in this bucket.
    max: T,

    /// Count of observations falling into this bucket.
    count: C,

}


/// An array of [`Bucket`]s which can themselves be a bucket.
///
/// [`Bucket`]: trait.Bucket.html
#[derive(Debug, Clone)]
struct Buckets<T: PartialOrd, B: Bucket<T>=LeafBucket<T>> {

    /// The upper bound of this `Bucket`.
    ///
    /// This should be the highest `max` value of the buckets herein contained.
    max: T,

    /// The aforementioned `Bucket`s herein contained.
    ///
    /// Currently, these must be in order.
    // FIXME: number of buckets per bucket set is hardcoded, currently -
    //        we don't need a `Vec` here, since the number of buckets never
    //        grows, but Rust doesn't have variable-sized arrays. we could
    //        just have a `Box<[B]>` here, or take a slice, but the former
    //        allocates and the latter would introduce a lifetime bound
    //        on the `Buckets` (which might be okay).
    buckets: [B; 5],

}

impl<T> LeafBucket<T> {

    // Construct a new `LeafBucket` with the upper bound of `max`.
    fn new<I: Into<T>>(max: I) -> Self {
        LeafBucket {
            max: max.into(),
            count: 0
        }
    }

}

impl<T> Bucket<T> for LeafBucket<T>
where
    T: PartialOrd
{

    fn contains(&self, t: &T) -> bool {
        t <= &self.max
    }

    fn add(&mut self, t: &T) -> bool {
        if self.contains(t) {
            // silently truncate if the count is over u32::MAX.
            self.count = self.count.saturating_add(1);
            true
        } else {
            false
        }

    }
}


impl<T, B> Bucket<T> for Buckets<T, B>
where
    T: PartialOrd,
    B: Bucket<T>,
{

    fn contains(&self, t: &T) -> bool {
        t <= &self.max
    }

    fn add(&mut self, t: &T) -> bool {
        // FIXME: this is assuming buckets is in order...
        for ref mut bucket in &mut self.buckets[..] {
            if bucket.add(t) { return true; }
        }
        false
    }

}

impl<T> Buckets<T, LeafBucket<T>>
where
    T: PartialOrd
{
    /// Construct a set of five linear [`LeafBucket`]s with width `width`.
    ///
    /// [`LeafBucket`]: struct.LeafBucket.html
    fn linear(width: u32) -> Self
    where T: From<u32> {
        Buckets {
            max: T::from(width * 5),
            buckets: [
                // TODO: construct this from a range argument?
                LeafBucket::new(width),
                LeafBucket::new(width * 2),
                LeafBucket::new(width * 3),
                LeafBucket::new(width * 4),
                LeafBucket::new(width * 5),
            ]
        }

    }
}

impl<'a, T> IntoIterator for &'a Buckets<T>
where
    T: PartialOrd,
{
    type Item = &'a LeafBucket<T>;
    type IntoIter = slice::Iter<'a, LeafBucket<T>>;

    fn into_iter(self) -> Self::IntoIter {
        self.buckets.into_iter()
    }

}

impl<'a, T> IntoIterator for &'a Buckets<T, Buckets<T>>
where
    T: PartialOrd,
{
    type Item = &'a LeafBucket<T>;
    // TODO: remove box
    type IntoIter = Box<Iterator<Item=Self::Item> + 'a>;

    fn into_iter(self) -> Self::IntoIter {
        Box::new(
            self.buckets
                .into_iter()
                .flat_map(|bucket| bucket.into_iter())
        )

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

// ===== impl Latencies =====

impl Latencies {

    /// Add an observed `Latency` value to the histogram.
    pub fn add<I: Into<Latency>>(&mut self, i: I) {
        self.0.add(&i.into());
    }

}


impl<I> ops::AddAssign<I> for Latencies
where
    I: Into<Latency>
{
    fn add_assign(&mut self, rhs: I) {
        self.add(rhs)
    }
}

impl Default for Latencies {
    fn default() -> Self {
        let max = Latency(500_000);
        // TODO: should be configurable, not hardcoded.
        // NOTE: All our bucket sizes should have one more zero than
        //       the controller's bucket sizes - controller will report
        //       data in whole-millisecond precision by multiplying all
        //       our latency values by 10.
        let buckets = [
            Buckets::linear(10),
            Buckets::linear(100),
            Buckets::linear(1_000),
            Buckets::linear(10_000),
            Buckets::linear(100_000),
        ]
        Latencies(Buckets { max, buckets })
    }
}

impl<'a> Into<Vec<LatencyProto>> for &'a Latencies {
    fn into(self) -> Vec<LatencyProto> {
        self.0.into_iter()
            .map(LatencyProto::from)
            .collect()
    }
}

impl Into<Vec<LatencyProto>> for Latencies {
    fn into(self) -> Vec<LatencyProto> {
        self.0.into_iter()
            .map(LatencyProto::from)
            .collect()
    }
}

// ===== impl LatencyProto =====

impl<'a> From<&'a LeafBucket<Latency>> for LatencyProto {
    fn from(bucket: &'a LeafBucket<Latency>) -> Self {
        LatencyProto {
            latency: bucket.max.into(),
            count: bucket.count,
        }
    }
}
