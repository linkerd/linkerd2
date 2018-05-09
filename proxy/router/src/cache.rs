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
    // TODO track access times for each entrys.
    pub fn access<Q>(&mut self, key: &Q) -> Option<&mut V>
    where
        Q: Hash + Equivalent<K>,
    {
        self.vals.get_mut(key)
    }

    /// Stores a route in the cache.
    ///
    /// If no capacity can be obtained an error is returned.
    pub fn store(&mut self, key: K, val: V) -> Result<(), CapacityExhausted> {
        self.reserve()?;
        self.vals.insert(key, val);

        Ok(())
    }

    /// Ensures that there is capacity to store an additional route.
    ///
    /// An error is returned if there is no available capacity.
    // TODO evict old entries
    pub fn reserve(&mut self) -> Result<usize, CapacityExhausted> {
        let avail = self.capacity - self.vals.len();
        if avail == 0 {
            // TODO If the cache is full, evict the oldest inactive route. If all
            // routes are active, fail the request.
            return Err(CapacityExhausted {
                capacity: self.capacity,
            });
        }

        Ok(avail)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use test_util::MultiplyAndAssign;

    #[test]
    fn store_and_reserve() {
        let mut cache = Cache::<_, MultiplyAndAssign>::new(2);

        assert_eq!(cache.reserve(), Ok(2));
        assert_eq!(cache.vals.len(), 0);

        assert_eq!(cache.store(1, MultiplyAndAssign::default()), Ok(()));
        assert_eq!(cache.reserve(), Ok(1));
        assert_eq!(cache.vals.len(), 1);

        assert_eq!(cache.store(2, MultiplyAndAssign::default()), Ok(()));
        assert_eq!(cache.reserve(), Err(CapacityExhausted { capacity: 2 }));
        assert_eq!(cache.vals.len(), 2);

        assert_eq!(
            cache.store(3, MultiplyAndAssign::default()),
            Err(CapacityExhausted { capacity: 2 })
        );
    }

    #[test]
    fn store_and_access() {
        let mut cache = Cache::<_, MultiplyAndAssign>::new(2);

        assert!(cache.access(&1).is_none());
        assert!(cache.access(&1).is_none());

        assert!(cache.store(1, MultiplyAndAssign::default()).is_ok());
        assert!(cache.access(&1).is_some());
        assert!(cache.access(&2).is_none());

        assert_eq!(cache.store(2, MultiplyAndAssign::default()), Ok(()));
        assert!(cache.access(&1).is_some());
        assert!(cache.access(&2).is_some());
    }
}
