use indexmap::IndexMap;
use std::{hash::Hash, ops::{Deref, DerefMut}, time::{Duration, Instant}};

// Reexported so IndexMap isn't exposed.
pub use indexmap::Equivalent;

/// An LRU cache
///
/// ## Assumptions
///
/// - `access` is common;
/// - `store` is less common;
/// - `capacity` is large enough that idle vals need not be removed frequently.
///
/// ## Complexity
///
/// - `access` computes in O(1) time (amortized average).
/// - `store` computes in O(1) time (average).
/// - `reserve` computes in O(n) time (average) when capacity is not available,
///
/// ### TODO
///
/// The underlying datastructure could be improved somewhat so that `reserve` can evict
/// unused nodes more efficiently. Given that eviction is intended to be rare, this is
/// probably not a very high priority.
pub struct Cache<K: Hash + Eq, V, N: Now = ()> {
    vals: IndexMap<K, Node<V>>,
    capacity: usize,
    max_idle_age: Duration,

    /// The time source.
    now: N,
}

/// Provides the current time within the module. Useful for testing.
pub trait Now {
    fn now(&self) -> Instant;
}

/// Wraps cache values so that each tracks its last access time.
#[derive(Debug, PartialEq)]
pub struct Node<T> {
    value: T,
    last_access: Instant,
}

/// A smart pointer that updates an access time when dropped.
///
/// Wraps a mutable reference to a `V`-typed value.
///
/// When the guard is dropped, the value's `last_access` time is updated with the provided
/// time source.
#[derive(Debug)]
pub struct Access<'a, T: 'a, N: Now + 'a = ()> {
    node: &'a mut Node<T>,
    now: &'a N,
}

/// A handle to a `Cache` that has capacity for at least one additional value.
#[derive(Debug)]
pub struct Reserve<'a, K: Hash + Eq + 'a, V: 'a, N: 'a> {
    vals: &'a mut IndexMap<K, Node<V>>,
    now: &'a N,
}

#[derive(Clone, Debug, PartialEq)]
pub struct CapacityExhausted {
    pub capacity: usize,
}

// ===== impl Cache =====

impl<K: Hash + Eq, V> Cache<K, V, ()> {
    pub fn new(capacity: usize, max_idle_age: Duration) -> Self {
        Self {
            capacity,
            vals: IndexMap::default(),
            max_idle_age,
            now: (),
        }
    }
}

impl<K: Hash + Eq, V, N: Now> Cache<K, V, N> {
    /// Accesses a route.
    ///
    /// A mutable reference to the route is wrapped in the returned `Access` to
    /// ensure that the access-time is updated when the reference is released.
    pub fn access<Q>(&mut self, key: &Q) -> Option<Access<V, N>>
    where
        Q: Hash + Equivalent<K>,
    {
        let v = self.vals.get_mut(key)?;
        Some(v.access(&self.now))
    }

    /// Ensures that there is capacity to store an additional route.
    ///
    /// Returns a handle that may be used to store an ite,. If there is no available
    /// capacity, idle entries may be evicted to create capacity.
    ///
    /// An error is returned if there is no available capacity.
    pub fn reserve(&mut self) -> Result<Reserve<K, V, N>, CapacityExhausted> {
        if self.vals.len() == self.capacity {
            // Only whole seconds are used to determine whether a node should be retained.
            // This is intended to prevent the need for repetitive reservations when
            // entries are clustered in tight time ranges.
            let max_age = self.max_idle_age.as_secs();
            let now = self.now.now();
            self.vals.retain(|_, n| {
                let age = now - n.last_access();
                age.as_secs() <= max_age
            });

            if self.vals.len() == self.capacity {
                return Err(CapacityExhausted {
                    capacity: self.capacity,
                });
            }
        }

        Ok(Reserve {
            vals: &mut self.vals,
            now: &self.now,
        })
    }

    /// Overrides the time source for tests.
    #[cfg(test)]
    fn with_clock<M: Now>(self, now: M) -> Cache<K, V, M> {
        Cache {
            now,
            vals: self.vals,
            capacity: self.capacity,
            max_idle_age: self.max_idle_age,
        }
    }
}

// ===== impl Reserve =====

impl<'a, K: Hash + Eq + 'a, V: 'a, N: Now + 'a> Reserve<'a, K, V, N> {
    /// Stores a route in the cache.
    pub fn store(self, key: K, val: V) {
        let node = Node::new(val.into(), self.now.now());
        self.vals.insert(key, node);
    }
}

// ===== impl Access =====

impl<'a, T: 'a, N: Now + 'a> Deref for Access<'a, T, N> {
    type Target = T;
    fn deref(&self) -> &Self::Target {
        &self.node
    }
}

impl<'a, T: 'a, N: Now + 'a> DerefMut for Access<'a, T, N> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.node
    }
}

impl<'a, T: 'a, N: Now + 'a> Access<'a, T, N> {
    #[cfg(test)]
    fn last_access(&self) -> Instant {
        self.node.last_access
    }
}

impl<'a, T: 'a, N: Now + 'a> Drop for Access<'a, T, N> {
    fn drop(&mut self) {
        self.node.last_access = self.now.now();
    }
}

// ===== impl Node =====

impl<T> Node<T> {
    pub fn new(value: T, last_access: Instant) -> Self {
        Node { value, last_access }
    }

    pub fn access<'a, N: Now + 'a>(&'a mut self, now: &'a N) -> Access<'a, T, N> {
        Access { now, node: self }
    }

    pub fn last_access(&self) -> Instant {
        self.last_access
    }
}

impl<T> Deref for Node<T> {
    type Target = T;
    fn deref(&self) -> &Self::Target {
        &self.value
    }
}

impl<T> DerefMut for Node<T> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.value
    }
}

// ===== impl Now =====

/// Default source of time.
impl Now for () {
    fn now(&self) -> Instant {
        Instant::now()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use futures::Future;
    use std::{cell::RefCell, rc::Rc, time::{Duration, Instant}};
    use test_util::MultiplyAndAssign;
    use tower_service::Service;

    /// A mocked instance of `Now` to drive tests.
    #[derive(Clone)]
    pub struct Clock(Rc<RefCell<Instant>>);

    // ===== impl Clock =====

    impl Default for Clock {
        fn default() -> Clock {
            Clock(Rc::new(RefCell::new(Instant::now())))
        }
    }

    impl Clock {
        pub fn advance(&mut self, d: Duration) {
            *self.0.borrow_mut() += d;
        }
    }

    impl Now for Clock {
        fn now(&self) -> Instant {
            self.0.borrow().clone()
        }
    }

    #[test]
    fn reserve_and_store() {
        let mut cache = Cache::<_, MultiplyAndAssign>::new(2, Duration::from_secs(1));

        {
            let r = cache.reserve().expect("reserve");
            r.store(1, MultiplyAndAssign::default());
        }
        assert_eq!(cache.vals.len(), 1);

        {
            let r = cache.reserve().expect("reserve");
            r.store(2, MultiplyAndAssign::default());
        }
        assert_eq!(cache.vals.len(), 2);

        assert_eq!(
            cache.reserve().err(),
            Some(CapacityExhausted { capacity: 2 })
        );
        assert_eq!(cache.vals.len(), 2);
    }

    #[test]
    fn store_and_access() {
        let mut cache = Cache::<_, MultiplyAndAssign>::new(2, Duration::from_secs(0));

        assert!(cache.access(&1).is_none());
        assert!(cache.access(&2).is_none());

        {
            let r = cache.reserve().expect("reserve");
            r.store(1, MultiplyAndAssign::default());
        }
        assert!(cache.access(&1).is_some());
        assert!(cache.access(&2).is_none());

        {
            let r = cache.reserve().expect("reserve");
            r.store(2, MultiplyAndAssign::default());
        }
        assert!(cache.access(&1).is_some());
        assert!(cache.access(&2).is_some());
    }

    #[test]
    fn reserve_does_nothing_when_capacity_exists() {
        let mut cache = Cache::<_, MultiplyAndAssign, _>::new(2, Duration::from_secs(0));

        // Create a route that goes idle immediately:
        {
            let r = cache.reserve().expect("capacity");
            let mut service = MultiplyAndAssign::default();
            service.call(1.into()).wait().unwrap();
            r.store(1, service);
        };
        assert_eq!(cache.vals.len(), 1);

        assert!(cache.reserve().is_ok());
        assert_eq!(cache.vals.len(), 1);
    }

    #[test]
    fn reserve_honors_max_idle_age() {
        let mut clock = Clock::default();
        let mut cache = Cache::<_, MultiplyAndAssign, _>::new(1, Duration::from_secs(2))
            .with_clock(clock.clone());

        // Touch `1` at 0s.
        cache
            .reserve()
            .expect("capacity")
            .store(1, MultiplyAndAssign::default());
        assert_eq!(
            cache.reserve().err(),
            Some(CapacityExhausted { capacity: 1 })
        );
        assert_eq!(cache.vals.len(), 1);

        // No capacity at 1s.
        clock.advance(Duration::from_secs(1));
        assert_eq!(
            cache.reserve().err(),
            Some(CapacityExhausted { capacity: 1 })
        );
        assert_eq!(cache.vals.len(), 1);

        // No capacity at 2s.
        clock.advance(Duration::from_secs(1));
        assert_eq!(
            cache.reserve().err(),
            Some(CapacityExhausted { capacity: 1 })
        );
        assert_eq!(cache.vals.len(), 1);

        // Capacity at 3+s.
        clock.advance(Duration::from_secs(1));
        assert!(cache.reserve().is_ok());
        assert_eq!(cache.vals.len(), 0);
    }

    #[test]
    fn last_access() {
        let mut clock = Clock::default();
        let mut cache =
            Cache::<_, MultiplyAndAssign>::new(1, Duration::from_secs(0)).with_clock(clock.clone());

        let t0 = clock.now();
        cache
            .reserve()
            .expect("capacity")
            .store(333, MultiplyAndAssign::default());

        clock.advance(Duration::from_secs(1));
        let t1 = clock.now();
        assert_eq!(cache.access(&333).map(|n| n.last_access()), Some(t0));

        clock.advance(Duration::from_secs(1));
        assert_eq!(cache.access(&333).map(|n| n.last_access()), Some(t1));
    }

    #[test]
    fn last_access_wiped_on_evict() {
        let mut clock = Clock::default();
        let mut cache =
            Cache::<_, MultiplyAndAssign>::new(1, Duration::from_secs(0)).with_clock(clock.clone());

        let t0 = clock.now();
        cache
            .reserve()
            .expect("capacity")
            .store(333, MultiplyAndAssign::default());

        clock.advance(Duration::from_secs(1));
        assert_eq!(cache.access(&333).map(|n| n.last_access()), Some(t0));

        // Cause the router to evict the `333` route.
        clock.advance(Duration::from_secs(1));
        cache
            .reserve()
            .expect("capacity")
            .store(444, MultiplyAndAssign::default());

        clock.advance(Duration::from_secs(1));
        let t1 = clock.now();
        cache
            .reserve()
            .expect("capacity")
            .store(333, MultiplyAndAssign::default());

        clock.advance(Duration::from_secs(1));
        assert_eq!(cache.access(&333).map(|n| n.last_access()), Some(t1));
    }

    #[test]
    fn node_access_updated_on_drop() {
        let mut clock = Clock::default();
        let t0 = clock.now();
        let mut node = Node::new(123, t0);

        clock.advance(Duration::from_secs(1));
        {
            let access = node.access(&clock);
            assert_eq!(access.last_access(), t0);
        }

        let t1 = clock.now();
        assert_eq!(node.last_access(), t1);
        assert_ne!(t0, t1);
    }
}
