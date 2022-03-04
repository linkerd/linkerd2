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
                    cidr: "10.1.0.0/24".to_string(),
                    except: None,
                },
                Network {
                    cidr: "10.1.1.0/24".to_string(),
                    except: Some(vec!["10.1.1.0/28".to_string()]),
                },
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_invalid_cidr() {
    admission::rejects(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec {
            networks: vec![Network {
                cidr: "10.1.0.0".to_string(),
                except: None,
            }],
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
                cidr: "10.1.0.0/16".to_string(),
                except: Some(vec!["10.1.1.1".to_string()]),
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_invalid_except() {
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
async fn rejects_except_whole_cidr() {
    admission::rejects(|ns| NetworkAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: NetworkAuthenticationSpec {
            networks: vec![Network {
                cidr: "10.1.1.0/24".to_string(),
                except: Some(vec!["10.1.0.0/16".to_string()]),
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
                cidr: "10.1.1.0/24".to_string(),
                except: Some(vec!["10.1.2.0/24".to_string()]),
            }],
        },
    })
    .await;
}
