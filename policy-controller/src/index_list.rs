use std::sync::Arc;

use kubert::index::IndexNamespacedResource;
use parking_lot::RwLock;

/// A list of indexes for a specific resource type.
///
/// An `IndexList` itself can then act as an index for that resource, and fans updates
/// out to each index in the list by cloning the update.
pub struct IndexList<A, T = A> {
    index: Arc<RwLock<A>>,
    tail: Option<T>,
}

impl<A, T, R> IndexNamespacedResource<R> for IndexList<A, T>
where
    A: IndexNamespacedResource<R>,
    T: IndexNamespacedResource<R>,
    R: Clone,
{
    fn apply(&mut self, resource: R) {
        if let Some(tail) = &mut self.tail {
            tail.apply(resource.clone());
        }
        self.index.write().apply(resource);
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Some(tail) = &mut self.tail {
            tail.delete(namespace.clone(), name.clone());
        }
        self.index.write().delete(namespace, name);
    }
}

impl<A, T> IndexList<A, T> {
    pub fn push<B>(self, index: Arc<RwLock<B>>) -> IndexList<B, IndexList<A, T>> {
        IndexList {
            index,
            tail: Some(self),
        }
    }

    pub fn shared(self) -> Arc<RwLock<Self>> {
        Arc::new(RwLock::new(self))
    }
}

impl<A> IndexList<A> {
    /// Returns a new `IndexList`.
    ///
    /// The second type parameter in the return value here can be anything that
    /// implements `IndexNamespacedResource<R>`, since it will just be `None`.
    /// Ideally, the type should be `!` (bottom) but `A` is conveniently available,
    /// so we use that.
    pub fn new(index: Arc<RwLock<A>>) -> IndexList<A, A> {
        IndexList { index, tail: None }
    }
}
