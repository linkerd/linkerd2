use linkerd_policy_controller_core::http_route::GroupKindName;

#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct ResourceId {
    pub namespace: String,
    pub name: String,
}

impl ResourceId {
    pub fn new(namespace: String, name: String) -> Self {
        Self { namespace, name }
    }
}

#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct NamespaceGroupKindName {
    pub namespace: String,
    pub gkn: GroupKindName,
}
