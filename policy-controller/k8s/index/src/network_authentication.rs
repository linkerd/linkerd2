use linkerd_policy_controller_core::NetworkMatch;
use linkerd_policy_controller_k8s_api::policy::NetworkAuthenticationSpec;

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub matches: Vec<NetworkMatch>,
}

impl TryFrom<NetworkAuthenticationSpec> for Spec {
    type Error = anyhow::Error;

    fn try_from(spec: NetworkAuthenticationSpec) -> anyhow::Result<Self> {
        let matches = spec
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

        if matches.is_empty() {
            anyhow::bail!("No networks configured");
        }

        Ok(Spec { matches })
    }
}
