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
