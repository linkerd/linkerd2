use std::sync::Arc;

use kube::ResourceExt;
use kubert::index::NamespacedRemoved;
use parking_lot::RwLock;
use prometheus_client::{
    encoding::EncodeLabelSet,
    metrics::{counter::Counter, family::Family, gauge::Gauge},
    registry::Registry,
};

pub struct IndexMetrics<T> {
    inner: T,

    index_size: Family<NamespacedIndexLabels, Gauge>,
    index_applies: Family<NamespacedIndexLabels, Counter>,
    index_deletes: Family<NamespacedIndexLabels, Counter>,
    index_resets: Family<IndexLabels, Counter>,
}

#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
struct NamespacedIndexLabels {
    namespace: String,
    kind: String,
}

#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
struct IndexLabels {
    kind: String,
}

pub trait SizedIndex<R> {
    fn size(&self, namespace: &str) -> usize;
}

impl<T, R> SizedIndex<R> for Arc<RwLock<T>>
where
    T: SizedIndex<R>,
{
    fn size(&self, namespace: &str) -> usize {
        self.read().size(namespace)
    }
}

impl<T> IndexMetrics<T> {
    pub fn register(inner: T, prom: &mut Registry) -> Self {
        let index_size = Family::default();
        prom.register(
            "index_size",
            "Gauge of the number of resources in the index",
            index_size.clone(),
        );

        let index_applies = Family::default();
        prom.register(
            "index_applies",
            "Count of applies to the index",
            index_applies.clone(),
        );

        let index_deletes = Family::default();
        prom.register(
            "index_deletes",
            "Counte of deletes to the index",
            index_deletes.clone(),
        );

        let index_resets = Family::default();
        prom.register(
            "index_resets",
            "Count of resets to the index",
            index_resets.clone(),
        );

        Self {
            inner,
            index_size,
            index_applies,
            index_deletes,
            index_resets,
        }
    }

    pub fn shared(self) -> Arc<RwLock<Self>> {
        Arc::new(RwLock::new(self))
    }
}

impl<R, T> kubert::index::IndexNamespacedResource<R> for IndexMetrics<Arc<RwLock<T>>>
where
    T: SizedIndex<R>,
    T: kubert::index::IndexNamespacedResource<R>,
    R: ResourceExt<DynamicType = ()>,
{
    /// Processes an update to a Kubernetes resource.
    fn apply(&mut self, resource: R) {
        let kind = R::kind(&());
        let namespace = resource.namespace().unwrap_or_default();
        self.index_applies
            .get_or_create(&NamespacedIndexLabels {
                namespace: namespace.clone(),
                kind: kind.to_string(),
            })
            .inc();
        self.inner.write().apply(resource);
        let size = self.inner.size(&namespace);
        self.index_size
            .get_or_create(&NamespacedIndexLabels {
                namespace,
                kind: kind.to_string(),
            })
            .set(size as i64);
    }

    /// Observes the removal of a Kubernetes resource.
    fn delete(&mut self, namespace: String, name: String) {
        let kind = R::kind(&());
        self.index_deletes
            .get_or_create(&NamespacedIndexLabels {
                namespace: namespace.clone(),
                kind: kind.to_string(),
            })
            .inc();
        self.inner.write().delete(namespace.clone(), name);
        let size = self.inner.size(&namespace);
        self.index_size
            .get_or_create(&NamespacedIndexLabels {
                namespace,
                kind: kind.to_string(),
            })
            .set(size as i64);
    }

    /// Resets an index with a set of live resources and a namespaced map of removed
    ///
    /// The default implementation calls `apply` and `delete`.
    fn reset(&mut self, resources: Vec<R>, removed: NamespacedRemoved) {
        let kind = R::kind(&());
        let namespaces = resources
            .iter()
            .flat_map(|r| r.namespace())
            .chain(removed.iter().map(|(namespace, _)| namespace.clone()))
            .collect::<Vec<_>>();
        self.index_resets
            .get_or_create(&IndexLabels {
                kind: kind.to_string(),
            })
            .inc();
        self.inner.write().reset(resources, removed);
        for ns in namespaces {
            let size = self.inner.size(&ns);
            self.index_size
                .get_or_create(&NamespacedIndexLabels {
                    namespace: ns,
                    kind: kind.to_string(),
                })
                .set(size as i64);
        }
    }
}
