use indexmap::{map, IndexMap};
use std::borrow::Borrow;
use std::hash::Hash;
use std::iter::IntoIterator;
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
    inner: IndexMap<K, V>,
    reset_on_next_modification: bool,
}

pub enum Exists<T> {
    /// Unknown if the item exists or not.
    Unknown,
    /// Affirmatively known to exist.
    Yes(T),
    /// Affirmatively known to not exist.
    No,
}

pub enum CacheChange {
    /// A new key was inserted.
    Insertion,
    /// A key-value pair was removed.
    Removal,
    /// The value mapped to an existing key was changed.
    Modification,
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
    V: PartialEq + Clone,
{
    pub fn new() -> Self {
        Cache {
            inner: IndexMap::new(),
            reset_on_next_modification: true,
        }
    }

    pub fn set_reset_on_next_modification(&mut self) {
        self.reset_on_next_modification = true;
    }

    /// Update the cache to contain the union of its current contents and the
    /// key-value pairs in `iter`. Pairs not present in the cache will be
    /// inserted, and keys present in both the cache and the iterator will be
    /// updated so that their inner match those in the iterator.
    pub fn update_union<I, F>(&mut self, iter: I, on_change: &mut F)
    where
        I: Iterator<Item = (K, V)>,
        F: FnMut((K, V), CacheChange),
    {
        fn update_inner<K, V, I, F>(
            inner: &mut IndexMap<K, V>,
            iter: I,
            on_change: &mut F
        )
        where
            K: Eq + Hash + Copy + Clone,
            V: PartialEq + Clone,
            I: Iterator<Item = (K, V)>,
            F: FnMut((K, V), CacheChange),
        {
            for (key, value) in iter {
                match inner.insert(key, value.clone()) {
                    // If the returned value is equal to the inserted value,
                    // then the inserted key was already present in the map
                    // and the value is unchanged. Do nothing.
                    Some(ref old_value) if old_value == &value => {},
                    // If `insert` returns `Some` with a different value than
                    // the one we inserted, then we changed the value for that
                    // key.
                    Some(_) =>
                        on_change((key, value), CacheChange::Modification),
                    // If `insert` returns `None`, then there was no old value
                    // previously present. Therefore, we inserted a new value
                    // into the cache.
                     None => on_change((key, value), CacheChange::Insertion),
                }
            }
        }

        if !self.reset_on_next_modification {
            update_inner(&mut self.inner, iter, on_change);
        } else {
            let to_insert = iter.collect::<IndexMap<K, V>>();
            update_inner(
                &mut self.inner,
                to_insert.iter().map(|(k, v)| (*k, v.clone())),
                on_change,
            );
            self.update_intersection(to_insert, on_change);
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
                if let Some(value) = self.inner.remove(&key) {
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
        self.update_intersection(IndexMap::new(), on_change)
    }

    /// Update the cache to contain the intersection of its current contents
    /// and the key-value pairs in `to_update`. Pairs not present in
    /// `to_update` will be removed from the cache, and any keys in the cache
    /// with different inner from those in `to_update` will be ovewritten to
    /// match `to_update`.
    pub fn update_intersection<F, Q>(
        &mut self,
        mut to_update: IndexMap<Q, V>,
        mut on_change: F
    )
    where
        F: FnMut((K, V), CacheChange),
        K: Borrow<Q>,
        Q: Hash + Eq,
    {
        self.inner.retain(|key, value| {
            match to_update.remove(key.borrow()) {
                // New value matches old value. Do nothing.
                Some(ref new_value) if new_value == value => true,
                // If the new value isn't equal to the old value, overwrite
                // the old value.
                Some(new_value) =>  {
                    let _ = mem::replace(value, new_value.clone());
                    on_change((*key, new_value), CacheChange::Modification);
                    true
                },
                // Key doesn't exist, remove it from the map.
                None => {
                    on_change((*key, value.clone()), CacheChange::Removal);
                    false
                }
            }
        });
        self.reset_on_next_modification = false;
    }
}

impl<'a, K, V> IntoIterator for &'a Cache<K, V>
where
    K: Hash + Eq,
{
    type IntoIter = map::Iter<'a, K, V>;
    type Item = <map::Iter<'a, K, V> as Iterator>::Item;

    fn into_iter(self) -> Self::IntoIter {
        self.inner.iter()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn update_union_reset_on_next_modification() {
        let original_values = indexmap!{ 1 => (), 2 => (), 3 => (), 4 => () };
        // One original value, one new value.
        let new_values = indexmap!{3 => (), 5 => ()};

        {
            let mut cache = Cache {
                inner: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.update_union(
                new_values.iter().map(|(&k, v)| (k, v.clone())),
                &mut |_, _| (),
            );
            assert_eq!(&cache.inner, &new_values);
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                inner: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.update_union(
                new_values.iter().map(|(&k, v)| (k, v.clone())),
                &mut |_, _| (),
            );
            assert_eq!(
                &cache.inner,
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
                inner: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.remove(to_remove.iter().map(|(&k, _)| k), &mut |_, _| ());
            assert_eq!(&cache.inner, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                inner: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.remove(to_remove.iter().map(|(&k, _)| k), &mut |_, _| ());
            assert_eq!(&cache.inner, &indexmap!{1 => (), 2 => (), 4 => ()});
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }

    #[test]
    fn clear_reset_on_next_modification() {
        let original_values = indexmap!{ 1 => (), 2 => (), 3 => (), 4 => () };

        {
            let mut cache = Cache {
                inner: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.clear(&mut |_, _| ());
            assert_eq!(&cache.inner, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                inner: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.clear(&mut |_, _| ());
            assert_eq!(&cache.inner, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }
}
