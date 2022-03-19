pub mod authorization_policy;
pub mod meshtls_authentication;
pub mod network_authentication;
pub mod server;
pub mod server_authorization;

pub use self::{
    authorization_policy::{AuthorizationPolicy, AuthorizationPolicySpec},
    meshtls_authentication::{MeshTLSAuthentication, MeshTLSAuthenticationSpec},
    network_authentication::{NetworkAuthentication, NetworkAuthenticationSpec},
    server::{Server, ServerSpec},
    server_authorization::{ServerAuthorization, ServerAuthorizationSpec},
};

/// Targets a resource--or resource type--within a the same namespace.
#[derive(
    Clone, Debug, Default, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
pub struct TargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: Option<String>,
    pub namespace: Option<String>,
}

impl TargetRef {
    /// Checks whether the target references the given resource type
    pub fn targets_kind<T>(&self) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        let dt = Default::default();

        let mut group = &*T::group(&dt);
        if group.is_empty() {
            group = "core";
        }

        self.group
            .as_deref()
            .unwrap_or("core")
            .eq_ignore_ascii_case(group)
            && self.kind.eq_ignore_ascii_case(&*T::kind(&dt))
    }

    /// Checks whether the target references the given namespaced resource
    pub fn targets_resource<T>(&self, resource: &T, default_ns: &str) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        if !self.targets_kind::<T>() {
            return false;
        }

        let tns = self.namespace.as_deref().unwrap_or(default_ns);
        let rns = resource.meta().namespace.as_deref().unwrap_or(default_ns);
        if !tns.eq_ignore_ascii_case(rns) {
            // If the resource specifies a namespace other than the target or the default
            // namespace, that's a deal-breaker.
            return false;
        }

        if let Some(name) = self.name.as_deref() {
            match resource.meta().name.as_deref() {
                None => return false,
                Some(rname) => {
                    if !rname.eq_ignore_ascii_case(name) {
                        return false;
                    }
                }
            }
        }

        true
    }

    /// Checks whether the target references the given cluster-level resource
    pub fn targets_cluster_resource<T>(&self, resource: &T) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        if !self.targets_kind::<T>() {
            return false;
        }

        if self.namespace.is_some() || resource.meta().namespace.is_some() {
            // If the reference or the resource has a namespace, that's a deal-breaker.
            return false;
        }

        if let Some(name) = self.name.as_deref() {
            match resource.meta().name.as_deref() {
                None => return false,
                Some(rname) => {
                    if !rname.eq_ignore_ascii_case(name) {
                        return false;
                    }
                }
            }
        }

        true
    }
}

#[cfg(test)]
mod tests {
    use super::{Server, TargetRef};
    use crate::{Namespace, ObjectMeta, ServiceAccount};

    #[test]
    fn targets_namespace() {
        let t = TargetRef {
            kind: "Namespace".to_string(),
            name: Some("appns".to_string()),
            ..TargetRef::default()
        };
        assert!(t.targets_kind::<Namespace>());
    }

    #[test]
    fn targets_service_account() {
        for tgt in &[
            TargetRef {
                kind: "ServiceAccount".to_string(),
                namespace: Some("appns".to_string()),
                name: Some("default".to_string()),
                ..TargetRef::default()
            },
            TargetRef {
                group: Some("core".to_string()),
                kind: "ServiceAccount".to_string(),
                namespace: Some("appns".to_string()),
                name: Some("default".to_string()),
                ..TargetRef::default()
            },
            TargetRef {
                group: Some("CORE".to_string()),
                kind: "SERVICEACCOUNT".to_string(),
                namespace: Some("APPNS".to_string()),
                name: Some("DEFAULT".to_string()),
                ..TargetRef::default()
            },
            TargetRef {
                kind: "ServiceAccount".to_string(),
                name: Some("default".to_string()),
                ..TargetRef::default()
            },
            TargetRef {
                kind: "ServiceAccount".to_string(),
                ..TargetRef::default()
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
                tgt.targets_resource(&sa, "appns"),
                "ServiceAccounts are targeted by name: {:#?}",
                tgt
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
                !tgt.targets_resource(&sa, "appns"),
                "ServiceAccounts in other namespaces should not be targeted: {:#?}",
                tgt
            );
        }

        let tgt = TargetRef {
            kind: "ServiceAccount".to_string(),
            name: Some("default".to_string()),
            ..TargetRef::default()
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
                !tgt.targets_resource(&sa, "appns")
            },
            "resource comparison uses "
        );
    }

    #[test]
    fn targets_server() {
        let tgt = TargetRef {
            group: Some("policy.linkerd.io".to_string()),
            kind: "Server".to_string(),
            namespace: Some("appns".to_string()),
            name: Some("http".to_string()),
            ..TargetRef::default()
        };

        assert!(tgt.targets_kind::<Server>());
    }
}
