use crate::index::Index;
use linkerd_policy_controller_core::NetworkMatch;
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};

impl kubert::index::IndexNamespacedResource<k8s::policy::NetworkAuthentication> for Index {
    fn apply(&mut self, authn: k8s::policy::NetworkAuthentication) {
        let namespace = authn.namespace().unwrap();
        let name = authn.name();

        let authns = authn
            .spec
            .networks
            .into_iter()
            .map(|n| NetworkMatch {
                net: n.cidr,
                except: n
                    .except
                    .into_iter()
                    .flatten()
                    .map(|e| e.into_net())
                    .collect(),
            })
            .collect::<Vec<_>>();

        if authns.is_empty() {
            tracing::warn!("No authentication targets");
            return;
        }

        self.apply_network_authentication(namespace, name, authns);
    }

    fn delete(&mut self, namespace: String, name: String) {
        self.delete_network_authentication(namespace, &name);
    }
}
