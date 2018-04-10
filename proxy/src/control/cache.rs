use indexmap::{map, IndexMap, IndexSet};
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
            inner: & mut IndexMap<K, V>,
            key: K,
            new_value: V,
            on_change: &mut F
        )
        where
            K: Eq + Hash + Copy,
            V: PartialEq,
            F: for<'value> FnMut(CacheChange<'value, K, V>),
        {
            if inner.get(&key)
                .map_or(false, |current| current == &new_value) {
                // The key is already mapped to the inserted value.
                // No change.
                return;
            } else {
                match inner.insert(key, new_value) {
                    Some(_) => on_change(CacheChange::Modification {
                        key,
                        new_value: inner.get(&key)
                            .expect("value was just inserted")
                    }),
                    // If `insert` returns `None`, then there was no old value
                    // previously present. Therefore, we inserted a new value
                    // into the cache.
                    None => on_change(CacheChange::Insertion {
                        key,
                        value: inner.get(&key)
                            .expect("value was just inserted")
                    }),
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
            // This retain is similar to `update_intersection`'s removal of
            // non-present keys but without updating values, since we've
            // already updated any values present in `to_insert`. We do this
            // instead of just calling `update_intersection` so we don't have
            // to clone the values we already know were inserted here.
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
        F: for<'value> FnMut(CacheChange<'value, K, V>),
        K: Borrow<Q>,
        Q: Hash + Eq,
        V: Clone,
    {
        self.inner.retain(|key, value| {
            match to_update.remove(key.borrow()) {
                // New value matches old value. Do nothing.
                Some(ref new_value) if new_value == value => true,
                // If the new value isn't equal to the old value, overwrite
                // the old value.
                Some(new_value) =>  {
                    // TODO: ideally, we wouldn't clone here, but we can't
                    // borrow the value from `self.inner` after inserting it,
                    // as `self.inner` is already borrowed mutably by `retain`.
                    // It would be nice to figure this out.
                    let _ = mem::replace(value, new_value.clone());
                    on_change(CacheChange::Modification {
                        key: *key,
                        new_value: &new_value,
                    });
                    true
                },
                // Key doesn't exist, remove it from the map.
                None => {
                    on_change(CacheChange::Removal { key: *key });
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
    fn update_intersection_fires_events() {
        let mut cache = Cache {
            inner: indexmap!{ 1 => "one",  2 => "two", 3 => "three" },
            reset_on_next_modification: false,
        };

        let mut removals = 0;
        let mut modifications = 0;

        cache.update_intersection(
            indexmap!{ 1 => "one", 3 => "three"},
            &mut |change: CacheChange<usize, &str>| {
                removals += 1;
                assert_eq!(change, CacheChange::Removal{ key: 2, });
            },
        );
        assert_eq!(cache.inner, indexmap!{ 1 => "one", 3 => "three"});
        assert_eq!(removals, 1);

        cache.update_intersection(
            indexmap!{ 3 => "3", 4 => "four" },
            &mut |change: CacheChange<usize, &str>| match change {
                CacheChange::Removal { key } => {
                    removals += 1;
                    assert_eq!(key, 1);
                }
                CacheChange::Insertion { ..  } => {
                    panic!("no insertion events should be fired");
                }
                CacheChange::Modification { key, new_value} => {
                    modifications += 1;
                    assert_eq!(key, 3);
                    assert_eq!(new_value, &"3")
                },
            }
        );
        assert_eq!(cache.inner, indexmap!{ 3 => "3" });
        assert_eq!(removals, 2);
        assert_eq!(modifications, 1);

        cache.update_intersection(
            indexmap!{ 5 => "five", 6 => "six" },
            &mut |change: CacheChange<usize, &str>| match change {
                CacheChange::Removal { key } => {
                    removals += 1;
                    assert_eq!(key, 3);
                }
                CacheChange::Insertion { ..  } => {
                    panic!("no insertion events should be fired");
                }
                CacheChange::Modification { ..  } => {
                    panic!("no more modification events should be fired");
                }
            }
        );

        assert!(cache.inner.is_empty());
        assert_eq!(removals, 3);
        assert_eq!(modifications, 1);
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
