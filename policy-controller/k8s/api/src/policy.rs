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
#[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
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

        self.group.as_deref().unwrap_or("core") == group && *self.kind == *T::kind(&dt)
    }

    /// Checks whether the target references the given namespaced resource
    pub fn targets_resource<T>(&self, resource: &T, default_ns: &str) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        use kube::ResourceExt;

        if !self.targets_kind::<T>() {
            return false;
        }

        if let Some(rns) = resource.namespace() {
            if rns != self.namespace.as_deref().unwrap_or(default_ns) {
                // If the resource specifies a namespace other than the target or the default
                // namespace, that's a deal-breaker.
                return false;
            }
        } else if let Some(ns) = self.namespace.as_deref() {
            if ns != default_ns {
                // If the resource does not specify a namespace but the target specifies a resource
                // other than the local default, that's a deal-breaker.
                return false;
            }
        }

        if let Some(name) = self.name.as_deref() {
            if name != resource.name() {
                return false;
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
        use kube::ResourceExt;

        if !self.targets_kind::<T>() {
            return false;
        }

        if self.namespace.is_some() || resource.namespace().is_some() {
            // If the reference or the resource has a namespace, that's a deal-breaker.
            return false;
        }

        if let Some(name) = self.name.as_deref() {
            if name != resource.name() {
                return false;
            }
        }

        true
    }
}

#[cfg(test)]
mod tests {
    use super::TargetRef;
    use k8s_openapi::api::core::v1::ServiceAccount;

    #[test]
    fn targets_service_account() {
        let t = TargetRef {
            kind: "ServiceAccount".to_string(),
            name: Some("default".to_string()),
            ..TargetRef::default()
        };
        assert!(t.targets_kind::<ServiceAccount>())
    }
}
