use indexmap::{IndexMap, IndexSet};
use indexmap::map::{self, Entry};
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

#[derive(Debug, Eq, PartialEq)]
pub enum CacheChange<'value, K, V: 'value> {
    /// A new key was inserted.
    Insertion { key: K, value: &'value V },
    /// A key-value pair was removed.
    Removal { key: K },
    /// The value mapped to an existing key was changed.
    Modification { key: K, new_value: &'value V },
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
    K: Copy + Hash + Eq,
    V: PartialEq,
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
        F: for<'value> FnMut(CacheChange<'value, K, V>),
    {
        fn update_inner<K, V, F>(
            inner: &mut IndexMap<K, V>,
            key: K,
            new_value: V,
            on_change: &mut F
        )
        where
            K: Eq + Hash + Copy,
            V: PartialEq,
            F: for<'value> FnMut(CacheChange<'value, K, V>),
        {
            match inner.entry(key) {
                Entry::Occupied(ref mut entry) if *entry.get() != new_value => {
                    on_change(CacheChange::Modification {
                        key,
                        new_value: &new_value,
                    });
                    entry.insert(new_value);
                },
                Entry::Vacant(entry) => {
                    on_change(CacheChange::Insertion {
                        key,
                        value: &new_value,
                    });
                    entry.insert(new_value);
                },
                Entry::Occupied(_) => {
                    // Entry is occupied but the value is the same as the new
                    // value, so skip it.
                }
            }
        }

        if !self.reset_on_next_modification {
            // We don't need to invalidate the cache, so just update
            // to add the new keys.
            iter.for_each(|(k, v)| {
                update_inner(&mut self.inner, k, v, on_change)
            });
        } else {
            // The cache was invalidated, so after updating entries present
            // in `to_insert`, remove any keys not present in `to_insert`.
            let retained_keys: IndexSet<K> = iter
                .map(|(k, v)| {
                    update_inner(&mut self.inner, k, v, on_change);
                    k
                })
                .collect();
            self.inner.retain(|key, _| if retained_keys.contains(key) {
                true
            } else {
                on_change(CacheChange::Removal { key: *key });
                false
            });
        }
        self.reset_on_next_modification = false;
    }

    pub fn remove<I, F>(&mut self, iter: I, on_change: &mut F)
    where
        I: Iterator<Item = K>,
        F: for<'value> FnMut(CacheChange<'value, K, V>),
    {
        if !self.reset_on_next_modification {
            for key in iter {
                if let Some(_) = self.inner.remove(&key) {
                    on_change(CacheChange::Removal { key });
                }
            }
        } else {
            self.clear(on_change);
        }
        self.reset_on_next_modification = false;
    }

    pub fn clear<F>(&mut self, on_change: &mut F)
    where
        F: for<'value> FnMut(CacheChange<'value, K, V>),
    {
        for (key, _) in self.inner.drain(..) {
            on_change(CacheChange::Removal { key })
        };
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
    fn update_union_fires_events() {
        let mut cache = Cache {
            inner: indexmap!{ 1 => "one",  2 => "two", },
            reset_on_next_modification: false,
        };

        let mut insertions = 0;
        let mut modifications = 0;

        cache.update_union(
            indexmap!{ 1 => "one", 2 => "2", 3 => "three"}.into_iter(),
            &mut |change: CacheChange<usize, &str>| match change {
                CacheChange::Removal { .. } => {
                    panic!("no removals should have been fired!");
                },
                CacheChange::Insertion { key, value } => {
                    insertions += 1;
                    assert_eq!(key, 3);
                    assert_eq!(value, &"three");
                },
                CacheChange::Modification { key, new_value } => {
                    modifications += 1;
                    assert_eq!(key, 2);
                    assert_eq!(new_value, &"2");
                }
            }
        );

        cache.update_union(
            indexmap!{ 1 => "1", 2 => "2", 3 => "3"}.into_iter(),
            &mut |change: CacheChange<usize, &str>| match change {
                CacheChange::Removal { .. } => {
                    panic!("no removals should have been fired!");
                },
                CacheChange::Insertion { .. } => {
                    panic!("no insertions should have been fired!");
                },
                CacheChange::Modification { key, new_value } => {
                    modifications += 1;
                    assert!(key == 1 || key == 3);
                    assert!(new_value == &"1" || new_value == &"3")
                }
            }
        );
        assert_eq!(insertions, 1);
        assert_eq!(modifications, 3);

        cache.update_union(
            indexmap!{ 4 => "four", 5 => "five"}.into_iter(),
            &mut |change: CacheChange<usize, &str>| match change {
                CacheChange::Removal { .. } => {
                    panic!("no removals should have been fired!");
                },
                CacheChange::Insertion { key, value } => {
                    insertions += 1;
                    assert!(key == 4 || key == 5);
                    assert!(value == &"four" || value == &"five")
                },
                CacheChange::Modification { .. } => {
                    panic!("no insertions should have been fired!");
                }
            }
        );
        assert_eq!(insertions, 3);
        assert_eq!(modifications, 3);
    }

    #[test]
    fn clear_fires_removal_event() {
        let mut cache = Cache {
            inner: indexmap!{ 1 => () },
            reset_on_next_modification: false,
        };
        cache.clear(
            &mut |change| {
                assert_eq!(change, CacheChange::Removal{ key: 1, });
            },
        )
    }

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
                &mut |_| (),
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
                &mut |_| (),
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
            cache.remove(to_remove.iter().map(|(&k, _)| k), &mut |_| ());
            assert_eq!(&cache.inner, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                inner: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.remove(to_remove.iter().map(|(&k, _)| k), &mut |_| ());
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
            cache.clear(&mut |_| ());
            assert_eq!(&cache.inner, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                inner: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.clear(&mut |_| ());
            assert_eq!(&cache.inner, &IndexMap::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }
}
