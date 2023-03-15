use std::sync::Arc;

use kubert::index::IndexNamespacedResource;
use parking_lot::RwLock;

pub struct IndexList<A, T> {
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
    pub fn new(index: Arc<RwLock<A>>) -> IndexList<A, A> {
        IndexList { index, tail: None }
    }

    pub fn add<B>(self, index: Arc<RwLock<B>>) -> IndexList<B, IndexList<A, T>> {
        IndexList {
            index,
            tail: Some(self),
        }
    }
}
