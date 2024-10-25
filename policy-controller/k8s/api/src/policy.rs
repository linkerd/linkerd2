pub mod authorization_policy;
pub mod egress_network;
pub mod httproute;
pub mod meshtls_authentication;
mod network;
pub mod network_authentication;
pub mod ratelimit_policy;
pub mod server;
pub mod server_authorization;
pub mod target_ref;

pub use self::{
    authorization_policy::{AuthorizationPolicy, AuthorizationPolicySpec},
    egress_network::{EgressNetwork, EgressNetworkSpec, EgressNetworkStatus, TrafficPolicy},
    httproute::{HttpRoute, HttpRouteSpec},
    meshtls_authentication::{MeshTLSAuthentication, MeshTLSAuthenticationSpec},
    network::{Cidr, Network},
    network_authentication::{NetworkAuthentication, NetworkAuthenticationSpec},
    ratelimit_policy::{HTTPLocalRateLimitPolicy, Limit, Override, RateLimitPolicySpec},
    server::{Server, ServerSpec},
    server_authorization::{ServerAuthorization, ServerAuthorizationSpec},
    target_ref::{ClusterTargetRef, LocalTargetRef, NamespacedTargetRef},
};

fn targets_kind<T>(group: Option<&str>, kind: &str) -> bool
where
    T: kube::Resource,
    T::DynamicType: Default,
{
    let dt = Default::default();

    let mut t_group = &*T::group(&dt);
    if t_group.is_empty() {
        t_group = "core";
    }

    group
        .filter(|s| !s.is_empty())
        .unwrap_or("core")
        .eq_ignore_ascii_case(t_group)
        && kind.eq_ignore_ascii_case(&T::kind(&dt))
}
