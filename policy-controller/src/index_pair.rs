use std::sync::Arc;

use kubert::index::IndexNamespacedResource;
use parking_lot::RwLock;

/// IndexPair holds a two indexes and forwards resource updates to both indexes
/// by cloning the update.
pub struct IndexPair<A, B> {
    first: Arc<RwLock<A>>,
    second: Arc<RwLock<B>>,
}

impl<A, B, R> IndexNamespacedResource<R> for IndexPair<A, B>
where
    A: IndexNamespacedResource<R>,
    B: IndexNamespacedResource<R>,
    R: Clone,
{
    fn apply(&mut self, resource: R) {
        self.first.write().apply(resource.clone());
        self.second.write().apply(resource);
    }

    fn delete(&mut self, namespace: String, name: String) {
        self.first.write().delete(namespace.clone(), name.clone());
        self.second.write().delete(namespace, name);
    }
}

impl<A, B> IndexPair<A, B> {
    pub fn shared(first: Arc<RwLock<A>>, second: Arc<RwLock<B>>) -> Arc<RwLock<Self>> {
        Arc::new(RwLock::new(Self { first, second }))
    }
}
