use std::{cmp, iter, slice};
use std::fmt::{self, Display};
use std::marker::PhantomData;

use super::{Counter, FmtMetric};

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
    sum: Counter,

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

/// Helper that lazily formats metric keys as {0}_{1}.
struct Key<A: Display, B: Display>(A, B);

/// Helper that lazily formats comma-separated labels `A,B`.
struct Labels<A: Display, B: Display>(A, B);

/// Helper that lazily formats an `{K}="{V}"`" label.
struct Label<K: Display, V: Display>(K, V);

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
            sum: Counter::default(),
            _p: PhantomData,
        }
    }

    pub fn add<U: Into<V>>(&mut self, u: U) {
        let v: V = u.into();
        let value: u64 = v.into();

        let idx = self.bounds.0.iter()
            .position(|b| match *b {
                Bucket::Le(ceiling) => value <= ceiling,
                Bucket::Inf => true,
            })
            .expect("all values must fit into a bucket");

        self.buckets[idx].incr();
        self.sum += value;
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

impl<V: Into<u64>> FmtMetric for Histogram<V> {
    const KIND: &'static str = "histogram";

    fn fmt_metric<N: Display>(&self, f: &mut fmt::Formatter, name: N) -> fmt::Result {
        let mut total = Counter::default();
        for (le, count) in self {
            total += *count;
            total.fmt_metric_labeled(f, Key(&name, "bucket"), Label("le", le))?;
        }
        total.fmt_metric(f, Key(&name, "count"))?;
        self.sum.fmt_metric(f, Key(&name, "sum"))?;

        Ok(())
    }

    fn fmt_metric_labeled<N, L>(&self, f: &mut fmt::Formatter, name: N, labels: L) -> fmt::Result
    where
        N: Display,
        L: Display,
    {
        let mut total = Counter::default();
        for (le, count) in self {
            total += *count;
            total.fmt_metric_labeled(f, Key(&name, "bucket"), Labels(&labels, Label("le", le)))?;
        }
        total.fmt_metric_labeled(f, Key(&name, "count"), &labels)?;
        self.sum.fmt_metric_labeled(f, Key(&name, "sum"), &labels)?;

        Ok(())
    }
}

// ===== impl Key =====

impl<A: Display, B: Display> fmt::Display for Key<A, B> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}_{}", self.0, self.1)
    }
}

// ===== impl Label =====

impl<K: Display, V: Display> fmt::Display for Label<K, V> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}=\"{}\"", self.0, self.1)
    }
}

// ===== impl Labels =====

impl<A: Display, B: Display> fmt::Display for Labels<A, B> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{},{}", self.0, self.1)
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

#[cfg(test)]
mod tests {
    use super::*;

    use std::u64;
    use std::collections::HashMap;

    const NUM_BUCKETS: usize = 47;
    static BOUNDS: &'static Bounds = &Bounds(&[
        Bucket::Le(10),
        Bucket::Le(20),
        Bucket::Le(30),
        Bucket::Le(40),
        Bucket::Le(50),
        Bucket::Le(60),
        Bucket::Le(70),
        Bucket::Le(80),
        Bucket::Le(90),
        Bucket::Le(100),
        Bucket::Le(200),
        Bucket::Le(300),
        Bucket::Le(400),
        Bucket::Le(500),
        Bucket::Le(600),
        Bucket::Le(700),
        Bucket::Le(800),
        Bucket::Le(900),
        Bucket::Le(1_000),
        Bucket::Le(2_000),
        Bucket::Le(3_000),
        Bucket::Le(4_000),
        Bucket::Le(5_000),
        Bucket::Le(6_000),
        Bucket::Le(7_000),
        Bucket::Le(8_000),
        Bucket::Le(9_000),
        Bucket::Le(10_000),
        Bucket::Le(20_000),
        Bucket::Le(30_000),
        Bucket::Le(40_000),
        Bucket::Le(50_000),
        Bucket::Le(60_000),
        Bucket::Le(70_000),
        Bucket::Le(80_000),
        Bucket::Le(90_000),
        Bucket::Le(100_000),
        Bucket::Le(200_000),
        Bucket::Le(300_000),
        Bucket::Le(400_000),
        Bucket::Le(500_000),
        Bucket::Le(600_000),
        Bucket::Le(700_000),
        Bucket::Le(800_000),
        Bucket::Le(900_000),
        Bucket::Le(1_000_000),
        Bucket::Inf,
    ]);

    quickcheck! {
        fn bucket_incremented(obs: u64) -> bool {
            let mut hist = Histogram::<u64>::new(&BOUNDS);
            hist.add(obs);
            let incremented_bucket = &BOUNDS.0.iter()
                .position(|bucket| match *bucket {
                    Bucket::Le(ceiling) => obs <= ceiling,
                    Bucket::Inf => true,
                })
                .unwrap();
            for i in 0..NUM_BUCKETS {
                let expected = if i == *incremented_bucket { 1 } else { 0 };
                let count: u64 = hist.buckets[i].into();
                assert_eq!(count, expected, "(for bucket <= {})", BOUNDS.0[i]);
            }
            true
        }

        fn sum_equals_total_of_observations(observations: Vec<u64>) -> bool {
            let mut hist = Histogram::<u64>::new(&BOUNDS);

            let mut expected_sum = Counter::default();
            for obs in observations {
                expected_sum += obs;
                hist.add(obs);
            }

            hist.sum == expected_sum
        }

        fn count_equals_number_of_observations(observations: Vec<u64>) -> bool {
            let mut hist = Histogram::<u64>::new(&BOUNDS);

            for obs in &observations {
                hist.add(*obs);
            }

            let count: u64 = hist.buckets.iter().map(|&c| {
                let count: u64 = c.into();
                count
            }).sum();
            count as usize == observations.len()
        }

        fn multiple_observations_increment_buckets(observations: Vec<u64>) -> bool {
            let mut buckets_and_counts: HashMap<usize, u64> = HashMap::new();
            let mut hist = Histogram::<u64>::new(&BOUNDS);

            for obs in observations {
                let incremented_bucket = &BOUNDS.0.iter()
                    .position(|bucket| match *bucket {
                        Bucket::Le(ceiling) => obs <= ceiling,
                        Bucket::Inf => true,
                    })
                    .unwrap();
                *buckets_and_counts
                    .entry(*incremented_bucket)
                    .or_insert(0) += 1;

                hist.add(obs);
            }

            for (i, count) in hist.buckets.iter().enumerate() {
                let count: u64 = (*count).into();
                assert_eq!(buckets_and_counts.get(&i).unwrap_or(&0), &count);
            }
            true
        }
    }
}

