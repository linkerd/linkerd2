use linkerd_policy_controller_core::http_route::GroupKindName;
use linkerd_policy_controller_k8s_api::{Resource, ResourceExt};

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

impl<'resource, Resrc> From<&'resource Resrc> for NamespaceGroupKindName
where
    Resrc: Resource<DynamicType = ()> + ResourceExt,
{
    fn from(resource: &'resource Resrc) -> Self {
        let (group, kind, name) = (
            Resrc::group(&()),
            Resrc::kind(&()),
            resource.name_unchecked(),
        );
        let namespace = resource
            .namespace()
            .unwrap_or_else(|| panic!("{} must have a namespace", kind));

        NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group,
                kind,
                name: name.into(),
            },
        }
    }
}
