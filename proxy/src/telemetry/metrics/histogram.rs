use std::num::Wrapping;
use std::{iter, ops, slice};

use super::Counter;

#[derive(Clone, Debug)]
pub struct Histogram<'bounds, O: 'bounds> {
    /// Reference to an array of upper bounds for observed values.
    ///
    /// The length of `bucket_bounds` must equal the length of `counts`.
    bucket_bounds: &'bounds [O],

    /// Array of buckets in which to count observed value.
    ///
    /// The upper bound of a given bucket `i` is given in `bucket_bounds[i]`.
    buckets: Box<[Counter]>,

    /// The total sum of all observed latency values.
    ///
    /// Histogram sums always explicitly wrap on overflows rather than
    /// panicking in debug builds. Prometheus' [`rate()`] and [`irate()`]
    /// queries handle breaks in monotonicity gracefully (see also
    /// [`resets()`]), so wrapping is less problematic than panicking in this
    /// case.
    ///
    /// Note, however, that Prometheus actually represents this using 64-bit
    /// floating-point numbers. The correct semantics are to ensure the sum
    /// always gets reset to zero after Prometheus reads it, before it would
    /// ever overflow a 52-bit `f64` mantissa.
    ///
    /// [`rate()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#rate()
    /// [`irate()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#irate()
    /// [`resets()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#resets
    ///
    // TODO: Implement Prometheus reset semantics correctly, taking into
    //       consideration that Prometheus represents this as `f64` and so
    //       there are only 52 significant bits.
    pub sum: Wrapping<u64>,
}

pub trait FormatSum: Sized {
    fn format_sum<'b>(&Histogram<'b, Self>) -> f64;
}

// ===== impl Histogram =====

impl<'bounds, O> Histogram<'bounds, O>
where
    O: Ord + Into<u64>,
    O: 'bounds,
{
    pub fn new(bucket_bounds: &'bounds [O],
               buckets: Box<[Counter]>)
               -> Self
    {
        assert_eq!(bucket_bounds.len(), buckets.len());
        Self {
            bucket_bounds,
            buckets,
            sum: Wrapping(0),
        }
    }

    /// Observe a measurement
    pub fn observe<I>(&mut self, measurement: I)
    where
        I: Into<O>,
    {
        let measurement = measurement.into();
        let i = self.bucket_bounds.iter()
            .position(|max| &measurement <= max)
            .expect("observed value greater than u32::MAX; this shouldn't be \
                     possible.");
        self.buckets[i].incr();
        self.sum += Wrapping(measurement.into());
    }
}

impl<'b, O, I> ops::AddAssign<I> for Histogram<'b, O>
where
    O: Ord + Into<u64>,
    O: 'b,
    I: Into<O>,
{
    #[inline]
    fn add_assign(&mut self, measurement: I) {
        self.observe(measurement)
    }

}

impl<'a, 'b, O> IntoIterator for &'a Histogram<'b, O>
where
    O: 'b,
{
    type Item = u64;
    type IntoIter = iter::Map<
        slice::Iter<'a, Counter>,
        fn(&'a Counter) -> u64
    >;

    fn into_iter(self) -> Self::IntoIter {
        self.buckets.iter().map(|&count| count.into())
    }

}

#[cfg(test)]
mod tests {
    use super::*;

    use std::collections::HashMap;
    use std::u64;

    const NUM_BUCKETS: usize = 47;
    static BOUNDS: [u64; NUM_BUCKETS] = [
        10, 20, 30, 40, 50, 60, 70, 80, 90, 100,
        200, 300, 400, 500, 600, 700, 800, 900, 1_000,
        2_000, 3_000, 4_000, 5_000, 6_000, 7_000, 8_000, 9_000, 10_000,
        20_000, 30_000, 40_000, 50_000, 60_000, 70_000, 80_000, 90_000, 100_000,
        200_000, 300_000, 400_000, 500_000, 600_000, 700_000, 800_000, 900_000, 1_000_000,
        u64::MAX
    ];

    quickcheck! {
        fn prop_count_incremented(obs: u64) -> bool {
            let mut hist = Histogram::new(
                &BOUNDS,
                Box::new([Counter::default(); NUM_BUCKETS])
            );
            hist += obs;
            let incremented_bucket = &BOUNDS.iter()
                .position(|bucket| &obs <= bucket)
                .unwrap();
            for i in 0..NUM_BUCKETS {
                let expected = if i == *incremented_bucket { 1 } else { 0 };
                let count: u64 = hist.buckets[i].into();
                assert_eq!(count, expected, "(for bucket <= {})", BOUNDS[i]);
            }
            true
        }

        fn prop_multiple_observations(observations: Vec<u64>) -> bool {
            let mut buckets_and_counts: HashMap<usize, u64> = HashMap::new();
            let mut hist = Histogram::new(
                &BOUNDS,
                Box::new([Counter::default(); NUM_BUCKETS])
            );

            for obs in observations {
                let incremented_bucket = &BOUNDS.iter()
                    .position(|bucket| obs <= *bucket)
                    .unwrap();
                buckets_and_counts.entry(*incremented_bucket)
                    .and_modify(|count| *count += 1)
                    .or_insert(1);

                hist += obs;
            }

            for (i, count) in hist.buckets.iter().enumerate() {
                let count: u64 = (*count).into();
                assert_eq!(buckets_and_counts.get(&i).unwrap_or(&0), &count);
            }
            true
        }
    }
}
