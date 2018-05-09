use indexmap::IndexMap;
use std::hash::Hash;

// Reexported so IndexMap isn't exposed.
pub use indexmap::Equivalent;

/// A cache for routes
///
/// ## Assumptions
///
/// - `access` is common;
/// - `store` is less common;
/// - `capacity` is large enough..
///
/// ## Complexity
///
/// - `access` computes in O(1) time (amortized average).
/// - `store` computes in O(1) time (average).
// TODO LRU
pub struct Cache<K: Hash + Eq, V> {
    vals: IndexMap<K, V>,
    capacity: usize,
}

pub struct Reserve<'a, K: Hash + Eq + 'a, V: 'a>(&'a mut Cache<K, V>);

#[derive(Clone, Debug, PartialEq)]
pub struct CapacityExhausted {
    pub capacity: usize,
}

impl<K: Hash + Eq, V> Cache<K, V> {
    pub fn new(capacity: usize) -> Self {
        Self {
            capacity,
            vals: IndexMap::default(),
        }
    }

    /// Accesses a route.
    // TODO track access times for each entry.
    pub fn access<Q>(&mut self, key: &Q) -> Option<&mut V>
    where
        Q: Hash + Equivalent<K>,
    {
        self.vals.get_mut(key)
    }

    /// Ensures that there is capacity to store an additional route.
    ///
    /// An error is returned if there is no available capacity.
    // TODO evict old entries
    pub fn reserve(&mut self) -> Result<Reserve<K, V>, CapacityExhausted> {
        let avail = self.capacity - self.vals.len();
        if avail == 0 {
            // TODO If the cache is full, evict the oldest inactive route. If all
            // routes are active, fail the request.
            return Err(CapacityExhausted {
                capacity: self.capacity,
            });
        }

        Ok(Reserve(self))
    }
}

impl<'a, K, V> Reserve<'a, K, V>
where
    K: Hash + Eq + 'a,
    V: 'a,
{
    /// Stores a route in the cache.
    ///
    /// If no capacity can be obtained an error is returned.
    pub fn store(self, key: K, val: V) {
        self.0.vals.insert(key, val);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use test_util::MultiplyAndAssign;

    #[test]
    fn reserve_and_store() {
        let mut cache = Cache::<_, MultiplyAndAssign>::new(2);

        cache.reserve().expect("reserve")
            .store(1, MultiplyAndAssign::default());
        assert_eq!(cache.vals.len(), 1);

        cache.reserve().expect("reserve")
            .store(2, MultiplyAndAssign::default());
        assert_eq!(cache.vals.len(), 2);

        assert_eq!(cache.reserve().err(), Some(CapacityExhausted { capacity: 2 }));
        assert_eq!(cache.vals.len(), 2);
    }

    #[test]
    fn store_and_access() {
        let mut cache = Cache::<_, MultiplyAndAssign>::new(2);

        assert!(cache.access(&1).is_none());
        assert!(cache.access(&2).is_none());

        cache.reserve().expect("reserve")
            .store(1, MultiplyAndAssign::default());
        assert!(cache.access(&1).is_some());
        assert!(cache.access(&2).is_none());

        cache.reserve().expect("reserve")
            .store(2, MultiplyAndAssign::default());
        assert!(cache.access(&1).is_some());
        assert!(cache.access(&2).is_some());
    }
}
