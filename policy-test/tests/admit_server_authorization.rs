use linkerd_policy_controller_k8s_api::{
    self as api,
    policy::server_authorization::{
        Client, MeshTls, Network, Server, ServerAuthorization, ServerAuthorizationSpec,
    },
};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| ServerAuthorization {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: ServerAuthorizationSpec {
            server: Server {
                name: Some("test".to_string()),
                selector: None,
            },
            client: Client {
                networks: Some(vec![
                    Network {
                        cidr: "10.1.0.0/24".parse().unwrap(),
                        except: None,
                    },
                    Network {
                        cidr: "10.1.1.0/24".parse().unwrap(),
                        except: Some(vec!["10.1.1.0/28".parse().unwrap()]),
                    },
                ]),
                unauthenticated: true,
                mesh_tls: None,
            },
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_except_whole_cidr() {
    admission::rejects(|ns| ServerAuthorization {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: ServerAuthorizationSpec {
            server: Server {
                name: Some("test".to_string()),
                selector: None,
            },
            client: Client {
                networks: Some(vec![Network {
                    cidr: "10.1.1.0/24".parse().unwrap(),
                    except: Some(vec!["10.1.0.0/16".parse().unwrap()]),
                }]),
                unauthenticated: true,
                mesh_tls: None,
            },
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_except_not_in_cidr() {
    admission::rejects(|ns| ServerAuthorization {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: ServerAuthorizationSpec {
            server: Server {
                name: Some("test".to_string()),
                selector: None,
            },
            client: Client {
                networks: Some(vec![Network {
                    cidr: "10.1.1.0/24".parse().unwrap(),
                    except: Some(vec!["10.1.2.0/24".parse().unwrap()]),
                }]),
                unauthenticated: true,
                mesh_tls: None,
            },
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
        kind = "ServerAuthorization",
        namespaced
    )]
    #[serde(rename_all = "camelCase")]
    pub struct ServerAuthorizationSpec {
        pub server: Server,
        pub client: Client,
    }

    #[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
    #[serde(rename_all = "camelCase")]
    pub struct Client {
        pub networks: Option<Vec<Network>>,

        #[serde(default)]
        pub unauthenticated: bool,

        #[serde(rename = "meshTLS")]
        pub mesh_tls: Option<MeshTls>,
    }

    #[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
    #[serde(rename_all = "camelCase")]
    pub struct Network {
        pub cidr: String,
        pub except: Option<Vec<String>>,
    }

    admission::rejects(|ns| ServerAuthorization {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: ServerAuthorizationSpec {
            server: Server {
                name: Some("test".to_string()),
                selector: None,
            },
            client: Client {
                networks: Some(vec![Network {
                    cidr: "bogus".to_string(),
                    except: None,
                }]),
                unauthenticated: true,
                mesh_tls: None,
            },
        },
    })
    .await;
}
