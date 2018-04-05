use indexmap::IndexMap;
use std::borrow::Borrow;
use std::hash::Hash;
use std::mem;

/// A key-value cache that supports incremental updates with lazy resetting
/// on invalidation.
///
/// When the cache `c` initially becomes invalid (i.e. it becomes
/// potentially out of sync with the data source so that incremental updates
/// would stop working), call `c.reset_on_next_modification()`; the next
/// incremental update will then replace the entire contents of the cache,
/// instead of incrementally augmenting it. Until that next modification,
/// however, the stale contents of the cache will be made available.
pub struct Cache<K, V> {
    values: IndexMap<K, V>,
    reset_on_next_modification: bool,
}

pub enum Exists<T> {
    /// Unknown if the item exists or not.
    Unknown,
    /// Affermatively known to exist.
    Yes(T),
    /// Affirmatively known to not exist.
    No,
}

pub enum CacheChange {
    Insertion,
    Removal,
}

// ===== impl Exists =====

impl<T> Exists<T> {
    pub fn take(&mut self) -> Exists<T> {
        mem::replace(self, Exists::Unknown)
    }
}

// ===== impl Cache =====

impl<K, V> Cache<K, V>
where
    K: Copy + Clone,
    K: Hash + Eq,
    V: Clone,
{
    pub fn new() -> Self {
        Cache {
            values: IndexMap::new(),
            reset_on_next_modification: true,
        }
    }

    pub fn values(&self) -> &IndexMap<K, V> {
        &self.values
    }

    pub fn set_reset_on_next_modification(&mut self) {
        self.reset_on_next_modification = true;
    }

    pub fn extend<I, F>(&mut self, iter: I, on_change: &mut F)
    where
        I: Iterator<Item = (K, V)>,
        F: FnMut((K, V), CacheChange),
    {
        fn extend_inner<K, V, I, F>(values: &mut IndexMap<K, V>, iter: I, on_change: &mut F)
        where
            K: Eq + Hash + Copy + Clone,
            V: Clone,
            I: Iterator<Item = (K, V)>,
            F: FnMut((K, V), CacheChange),
        {
            for (key, value) in iter {
                if values.insert(key, value.clone()).is_none() {
                    on_change((key, value), CacheChange::Insertion);
                }
            }
        }

        if !self.reset_on_next_modification {
            extend_inner(&mut self.values, iter, on_change);
        } else {
            let to_insert = iter.collect::<IndexMap<K, V>>();
            extend_inner(
                &mut self.values,
                to_insert.iter().map(|(k, v)| (k.clone(), v.clone())),
                on_change,
            );
            self.retain(&to_insert, on_change);
        }
        self.reset_on_next_modification = false;
    }

    pub fn remove<I, F>(&mut self, iter: I, on_change: &mut F)
    where
        I: Iterator<Item = K>,
        F: FnMut((K, V), CacheChange),
    {
        if !self.reset_on_next_modification {
            for key in iter {
                if let Some(value) = self.values.remove(&key) {
                    on_change((key, value), CacheChange::Removal);
                }
            }
        } else {
            self.clear(on_change);
        }
        self.reset_on_next_modification = false;
    }

    pub fn clear<F>(&mut self, on_change: &mut F)
    where
        F: FnMut((K, V), CacheChange),
    {
        self.retain(&IndexMap::new(), on_change)
    }

    pub fn retain<F, Q>(&mut self, to_retain: &IndexMap<K, V>, mut on_change: F)
    where
        F: FnMut((K, V), CacheChange),
        K: Borrow<Q>,
        Q: Hash + Eq,
    {
        self.values.retain(|key, value| {
            let retain = to_retain.contains_key(key.borrow());
            if !retain {
                on_change((*key, value.clone()), CacheChange::Removal)
            }
            retain
        });
        self.reset_on_next_modification = false;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn extend_reset_on_next_modification() {
        let original_values = indexmap!{ 1 => (), 2 => (), 3 => (), 4 => () };
        // One original value, one new value.
        let new_values = indexmap!{3 => (), 5 => ()};

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.extend(
                new_values.iter().map(|(&k, v)| (k, v.clone())),
                &mut |_, _| (),
            );
            assert_eq!(&cache.values, &new_values);
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.extend(
                new_values.iter().map(|(&k, v)| (k, v.clone())),
                &mut |_, _| (),
            );
            assert_eq!(
                &cache.values,
                &indexmap!{ 1 => (), 2 => (), 3 => (), 4 => (), 5 => () }
            );
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }

    #[test]
    fn remove_reset_on_next_modification() {
        let original_values = indexmap!{ 1 => (), 2 => (), 3 => (), 4 => () };

        // One original value, one new value.
        let to_remove = indexmap!{ 3 => (), 5 => ()};

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.remove(to_remove.iter().map(|(&k, _)| k), &mut |_, _| ());
            assert_eq!(&cache.values, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.remove(to_remove.iter().map(|(&k, _)| k), &mut |_, _| ());
            assert_eq!(&cache.values, &indexmap!{1 => (), 2 => (), 4 => ()});
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }

    #[test]
    fn clear_reset_on_next_modification() {
        let original_values = indexmap!{ 1 => (), 2 => (), 3 => (), 4 => () };

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.clear(&mut |_, _| ());
            assert_eq!(&cache.values, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.clear(&mut |_, _| ());
            assert_eq!(&cache.values, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }
}
