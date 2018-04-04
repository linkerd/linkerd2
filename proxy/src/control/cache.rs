use std;
use std::collections::HashSet;
use std::mem;

/// A cache that supports incremental updates with lazy resetting on
/// invalidation.
///
/// When the cache `c` initially becomes invalid (i.e. it becomes
/// potentially out of sync with the data source so that incremental updates
/// would stop working), call `c.reset_on_next_modification()`; the next
/// incremental update will then replace the entire contents of the cache,
/// instead of incrementally augmenting it. Until that next modification,
/// however, the stale contents of the cache will be made available.
pub struct Cache<T> {
    values: HashSet<T>,
    reset_on_next_modification: bool,
}

pub enum Exists<T> {
    Unknown, // Unknown if the item exists or not
    Yes(T),
    No, // Affirmatively known to not exist.
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

impl<T> Cache<T> where T: Clone + Copy + Eq + std::hash::Hash {
    pub fn new() -> Self {
        Cache {
            values: HashSet::new(),
            reset_on_next_modification: true,
        }
    }

    pub fn values(&self) -> &HashSet<T> {
        &self.values
    }

    pub fn set_reset_on_next_modification(&mut self) {
        self.reset_on_next_modification = true;
    }

    pub fn extend<I, F>(&mut self, iter: I, on_change: &mut F)
        where I: Iterator<Item = T>,
              F: FnMut(T, CacheChange),
    {
        fn extend_inner<T, I, F>(values: &mut HashSet<T>, iter: I, on_change: &mut F)
            where T: Copy + Eq + std::hash::Hash, I: Iterator<Item = T>, F: FnMut(T, CacheChange)
        {
            for value in iter {
                if values.insert(value) {
                    on_change(value, CacheChange::Insertion);
                }
            }
        }

        if !self.reset_on_next_modification {
            extend_inner(&mut self.values, iter, on_change);
        } else {
            let to_insert = iter.collect::<HashSet<T>>();
            extend_inner(&mut self.values, to_insert.iter().map(|value| *value), on_change);
            self.retain(&to_insert, on_change);
        }
        self.reset_on_next_modification = false;
    }

    pub fn remove<I, F>(&mut self, iter: I, on_change: &mut F)
        where I: Iterator<Item = T>,
              F: FnMut(T, CacheChange)
    {
        if !self.reset_on_next_modification {
            for value in iter {
                if self.values.remove(&value) {
                    on_change(value, CacheChange::Removal);
                }
            }
        } else {
            self.clear(on_change);
        }
        self.reset_on_next_modification = false;
    }

    pub fn clear<F>(&mut self, on_change: &mut F) where F: FnMut(T, CacheChange) {
        self.retain(&HashSet::new(), on_change)
    }

    pub fn retain<F>(&mut self, to_retain: &HashSet<T>, mut on_change: F)
        where F: FnMut(T, CacheChange)
    {
        self.values.retain(|value| {
            let retain = to_retain.contains(&value);
            if !retain {
                on_change(*value, CacheChange::Removal)
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
        let original_values = [1, 2, 3, 4].iter().cloned().collect::<HashSet<usize>>();

        // One original value, one new value.
        let new_values = [3, 5].iter().cloned().collect::<HashSet<usize>>();

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.extend(new_values.iter().cloned(), &mut |_, _| ());
            assert_eq!(&cache.values, &new_values);
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.extend(new_values.iter().cloned(), &mut |_, _| ());
            assert_eq!(&cache.values,
                       &[1, 2, 3, 4, 5].iter().cloned().collect::<HashSet<usize>>());
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }

    #[test]
    fn remove_reset_on_next_modification() {
        let original_values = [1, 2, 3, 4].iter().cloned().collect::<HashSet<usize>>();

        // One original value, one new value.
        let to_remove = [3, 5].iter().cloned().collect::<HashSet<usize>>();

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.remove(to_remove.iter().cloned(), &mut |_, _| ());
            assert_eq!(&cache.values, &HashSet::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.remove(to_remove.iter().cloned(), &mut |_, _| ());
            assert_eq!(&cache.values, &[1, 2, 4].iter().cloned().collect::<HashSet<usize>>());
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }

    #[test]
    fn clear_reset_on_next_modification() {
        let original_values = [1, 2, 3, 4].iter().cloned().collect::<HashSet<usize>>();

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: true,
            };
            cache.clear(&mut |_, _| ());
            assert_eq!(&cache.values, &HashSet::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }

        {
            let mut cache = Cache {
                values: original_values.clone(),
                reset_on_next_modification: false,
            };
            cache.clear(&mut |_, _| ());
            assert_eq!(&cache.values, &HashSet::new());
            assert_eq!(cache.reset_on_next_modification, false);
        }
    }
}
