use super::targets_kind;

#[derive(
    Clone, Debug, Eq, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
pub struct ClusterTargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: String,
}

#[derive(
    Clone, Debug, Eq, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
pub struct LocalTargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: String,
}

#[derive(
    Clone, Debug, Eq, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
pub struct NamespacedTargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: String,
    pub namespace: Option<String>,
}

impl ClusterTargetRef {
    pub fn from_resource<T>(resource: &T) -> Self
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        let (group, kind, name) = group_kind_name(resource);
        Self { group, kind, name }
    }

    /// Returns the target ref kind, qualified by its group, if necessary.
    pub fn canonical_kind(&self) -> String {
        canonical_kind(self.group.as_deref(), &self.kind)
    }

    /// Checks whether the target references the given resource type
    pub fn targets_kind<T>(&self) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        targets_kind::<T>(self.group.as_deref(), &self.kind)
    }

    /// Checks whether the target references the given cluster-level resource
    pub fn targets<T>(&self, resource: &T) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        if !self.targets_kind::<T>() {
            return false;
        }

        if resource.meta().namespace.is_some() {
            // If the reference or the resource has a namespace, that's a deal-breaker.
            return false;
        }

        match resource.meta().name.as_deref() {
            None => return false,
            Some(rname) => {
                if !self.name.eq_ignore_ascii_case(rname) {
                    return false;
                }
            }
        }

        true
    }
}

impl LocalTargetRef {
    pub fn from_resource<T>(resource: &T) -> Self
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        let (group, kind, name) = group_kind_name(resource);
        Self { group, kind, name }
    }

    /// Returns the target ref kind, qualified by its group, if necessary.
    pub fn canonical_kind(&self) -> String {
        canonical_kind(self.group.as_deref(), &self.kind)
    }

    /// Checks whether the target references the given resource type
    pub fn targets_kind<T>(&self) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        targets_kind::<T>(self.group.as_deref(), &self.kind)
    }

    /// Checks whether the target references the given namespaced resource
    pub fn targets<T>(&self, resource: &T, local_ns: &str) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        if !self.targets_kind::<T>() {
            return false;
        }

        // If the resource specifies a namespace other than the target or the
        // default namespace, that's a deal-breaker.
        match resource.meta().namespace.as_deref() {
            Some(rns) if rns.eq_ignore_ascii_case(local_ns) => {}
            _ => return false,
        };

        match resource.meta().name.as_deref() {
            Some(rname) => rname.eq_ignore_ascii_case(&self.name),
            _ => false,
        }
    }
}

impl NamespacedTargetRef {
    pub fn from_resource<T>(resource: &T) -> Self
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        let (group, kind, name) = group_kind_name(resource);
        let namespace = resource.meta().namespace.clone();
        Self {
            group,
            kind,
            name,
            namespace,
        }
    }

    /// Returns the target ref kind, qualified by its group, if necessary.
    pub fn canonical_kind(&self) -> String {
        canonical_kind(self.group.as_deref(), &self.kind)
    }

    /// Checks whether the target references the given resource type
    pub fn targets_kind<T>(&self) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        targets_kind::<T>(self.group.as_deref(), &self.kind)
    }

    /// Checks whether the target references the given namespaced resource
    pub fn targets<T>(&self, resource: &T, local_ns: &str) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        if !self.targets_kind::<T>() {
            return false;
        }

        // If the resource specifies a namespace other than the target or the
        // default namespace, that's a deal-breaker.
        let tns = self.namespace.as_deref().unwrap_or(local_ns);
        match resource.meta().namespace.as_deref() {
            Some(rns) if rns.eq_ignore_ascii_case(tns) => {}
            _ => return false,
        };

        match resource.meta().name.as_deref() {
            None => return false,
            Some(rname) => {
                if !self.name.eq_ignore_ascii_case(rname) {
                    return false;
                }
            }
        }

        true
    }
}

fn canonical_kind(group: Option<&str>, kind: &str) -> String {
    if let Some(group) = group {
        format!("{kind}.{group}")
    } else {
        kind.to_string()
    }
}

fn group_kind_name<T>(resource: &T) -> (Option<String>, String, String)
where
    T: kube::Resource,
    T::DynamicType: Default,
{
    let dt = Default::default();

    let group = match T::group(&dt) {
        g if (*g).is_empty() => None,
        g => Some(g.to_string()),
    };

    let kind = T::kind(&dt).to_string();

    let name = resource
        .meta()
        .name
        .clone()
        .expect("resource must have a name");

    (group, kind, name)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{policy::Server, Namespace, ObjectMeta, ServiceAccount};

    #[test]
    fn cluster_targets_namespace() {
        let t = ClusterTargetRef {
            group: None,
            kind: "Namespace".to_string(),
            name: "appns".to_string(),
        };
        assert!(t.targets_kind::<Namespace>());
        assert!(t.targets(&Namespace {
            metadata: ObjectMeta {
                name: Some("appns".to_string()),
                ..ObjectMeta::default()
            },
            ..Namespace::default()
        }));
    }

    #[test]
    fn namespaced_targets_service_account() {
        for tgt in &[
            NamespacedTargetRef {
                group: None,
                kind: "ServiceAccount".to_string(),
                name: "default".to_string(),
                namespace: Some("appns".to_string()),
            },
            NamespacedTargetRef {
                group: Some("core".to_string()),
                kind: "ServiceAccount".to_string(),
                name: "default".to_string(),
                namespace: Some("appns".to_string()),
            },
            NamespacedTargetRef {
                group: Some("CORE".to_string()),
                kind: "SERVICEACCOUNT".to_string(),
                name: "DEFAULT".to_string(),
                namespace: Some("APPNS".to_string()),
            },
            NamespacedTargetRef {
                group: None,
                kind: "ServiceAccount".to_string(),
                name: "default".to_string(),
                namespace: None,
            },
        ] {
            assert!(tgt.targets_kind::<ServiceAccount>());

            assert!(!tgt.targets_kind::<Namespace>());

            let sa = ServiceAccount {
                metadata: ObjectMeta {
                    namespace: Some("appns".to_string()),
                    name: Some("default".to_string()),
                    ..ObjectMeta::default()
                },
                ..ServiceAccount::default()
            };
            assert!(
                tgt.targets(&sa, "appns"),
                "ServiceAccounts are targeted by name: {tgt:#?}"
            );

            let sa = ServiceAccount {
                metadata: ObjectMeta {
                    namespace: Some("otherns".to_string()),
                    name: Some("default".to_string()),
                    ..ObjectMeta::default()
                },
                ..ServiceAccount::default()
            };
            assert!(
                !tgt.targets(&sa, "appns"),
                "ServiceAccounts in other namespaces should not be targeted: {tgt:#?}"
            );
        }

        let tgt = NamespacedTargetRef {
            group: None,
            kind: "ServiceAccount".to_string(),
            name: "default".to_string(),
            namespace: None,
        };
        assert!(
            {
                let sa = ServiceAccount {
                    metadata: ObjectMeta {
                        namespace: Some("appns".to_string()),
                        name: Some("special".to_string()),
                        ..ObjectMeta::default()
                    },
                    ..ServiceAccount::default()
                };
                !tgt.targets(&sa, "appns")
            },
            "resource comparison uses "
        );
    }

    #[test]
    fn namespaced_targets_server() {
        let tgt = NamespacedTargetRef {
            group: Some("policy.linkerd.io".to_string()),
            kind: "Server".to_string(),
            name: "http".to_string(),
            namespace: Some("appns".to_string()),
        };

        assert!(tgt.targets_kind::<Server>());
    }
}
