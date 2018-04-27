use std::{cmp, fmt, iter, slice};
use std::num::Wrapping;
use std::marker::PhantomData;

use super::Counter;

/// A series of latency values and counts.
#[derive(Debug, Clone)]
pub struct Histogram<V: Into<u64>> {
    bounds: &'static Bounds,
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
    // TODO: Implement Prometheus reset semantics correctly, taking into consideration
    //       that Prometheus represents this as `f64` and so there are only 52 significant
    //       bits.
    pub sum: Wrapping<u64>,

    _p: PhantomData<V>,
}

#[derive(Debug, Eq, PartialEq, Copy, Clone, Hash)]
pub enum Bucket {
    Le(u64),
    Inf,
}

/// A series of increasing Buckets values.
#[derive(Debug)]
pub struct Bounds(pub &'static [Bucket]);

// ===== impl Histogram =====

impl<V: Into<u64>> Histogram<V> {
    pub fn new(bounds: &'static Bounds) -> Self {
        let mut buckets = Vec::with_capacity(bounds.0.len());
        let mut prior = &Bucket::Le(0);
        for bound in bounds.0.iter() {
            assert!(prior < bound);
            buckets.push(Counter::default());
            prior = bound;
        }

        Self {
            bounds,
            buckets: buckets.into_boxed_slice(),
            sum: Wrapping(0),
            _p: PhantomData,
        }
    }

    pub fn add(&mut self, v: V) {
        let value = v.into();

        let idx = self.bounds.0.iter()
            .position(|b| match *b {
                Bucket::Le(ceiling) => value <= ceiling,
                Bucket::Inf => true,
            })
            .expect("all values must fit into a bucket");

        self.buckets[idx].incr();
        self.sum += Wrapping(value);
    }

    pub fn sum(&self) -> u64 {
        self.sum.0
    }
}

impl<'a, V: Into<u64>> IntoIterator for &'a Histogram<V> {
    type Item = (&'a Bucket, &'a Counter);
    type IntoIter = iter::Zip<
        slice::Iter<'a, Bucket>,
        slice::Iter<'a, Counter>,
    >;

    fn into_iter(self) -> Self::IntoIter {
        self.bounds.0.iter().zip(self.buckets.iter())
    }
}

// ===== impl Bucket =====

impl fmt::Display for Bucket {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            Bucket::Le(v) => write!(f, "{}", v),
            Bucket::Inf => write!(f, "+Inf"),
        }
    }
}

impl cmp::PartialOrd<Bucket> for Bucket {
    fn partial_cmp(&self, rhs: &Bucket) -> Option<cmp::Ordering> {
        Some(self.cmp(rhs))
    }
}

impl cmp::Ord for Bucket {
    fn cmp(&self, rhs: &Bucket) -> cmp::Ordering {
        match (*self, *rhs) {
            (Bucket::Le(s), Bucket::Le(r)) => s.cmp(&r),
            (Bucket::Le(_), Bucket::Inf) => cmp::Ordering::Less,
            (Bucket::Inf, Bucket::Le(_)) => cmp::Ordering::Greater,
            (Bucket::Inf, Bucket::Inf) => cmp::Ordering::Equal,
        }
    }
}
