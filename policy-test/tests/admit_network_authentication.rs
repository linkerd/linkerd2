use linkerd_policy_controller_k8s_api::{
    self as api,
    policy::network_authentication::{Network, NetworkAuthentication, NetworkAuthenticationSpec},
};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec {
            networks: vec![
                Network {
                    cidr: "10.1.0.0/24".parse().unwrap(),
                    except: None,
                },
                Network {
                    cidr: "10.1.1.0/24".parse().unwrap(),
                    except: Some(vec!["10.1.1.0/28".parse().unwrap()]),
                },
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_ip_except() {
    admission::accepts(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec {
            networks: vec![Network {
                cidr: "10.1.0.0/16".parse().unwrap(),
                except: Some(vec!["10.1.1.1".parse().unwrap()]),
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_except_whole_cidr() {
    admission::rejects(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec {
            networks: vec![Network {
                cidr: "10.1.1.0/24".parse().unwrap(),
                except: Some(vec!["10.1.0.0/16".parse().unwrap()]),
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_except_not_in_cidr() {
    admission::rejects(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec {
            networks: vec![Network {
                cidr: "10.1.1.0/24".parse().unwrap(),
                except: Some(vec!["10.1.2.0/24".parse().unwrap()]),
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_invalid_cidr() {
    // Duplicate the CRD with relaxed validation so we can send an invalid CIDR value.
    #[derive(
        Clone,
        Debug,
        Default,
        kube::CustomResource,
        serde::Deserialize,
        serde::Serialize,
        schemars::JsonSchema,
    )]
    #[kube(
        group = "policy.linkerd.io",
        version = "v1alpha1",
        kind = "NetworkAuthentication",
        namespaced
    )]
    #[serde(rename_all = "camelCase")]
    pub struct NetworkAuthenticationSpec {
        pub networks: Vec<Network>,
    }

    #[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
    #[serde(rename_all = "camelCase")]
    pub struct Network {
        pub cidr: String,
        pub except: Option<Vec<String>>,
    }

    admission::rejects(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec {
            networks: vec![Network {
                cidr: "10.1.0.0/16".to_string(),
                except: Some(vec!["bogus".to_string()]),
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_empty() {
    admission::rejects(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec { networks: vec![] },
    })
    .await;
}
