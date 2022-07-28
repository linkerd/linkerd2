use linkerd_policy_controller_k8s_api::{
    self as api, labels,
    policy::server::{Port, ProxyProtocol, Server, ServerSpec},
};
use linkerd_policy_test::{admission, with_temp_ns};

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| Server {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: ServerSpec {
            pod_selector: api::labels::Selector::default(),
            port: Port::Number(80.try_into().unwrap()),
            proxy_protocol: None,
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_server_updates() {
    with_temp_ns(|client, ns| async move {
        let test0 = Server {
            metadata: api::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some("test0".to_string()),
                ..Default::default()
            },
            spec: ServerSpec {
                pod_selector: api::labels::Selector::from_iter(Some(("app", "test"))),
                port: Port::Number(80.try_into().unwrap()),
                proxy_protocol: None,
            },
        };

        let api = kube::Api::namespaced(client, &*ns);
        api.create(&kube::api::PostParams::default(), &test0)
            .await
            .expect("resource must apply");

        api.patch(
            "test0",
            &kube::api::PatchParams::default(),
            &kube::api::Patch::Merge(test0),
        )
        .await
        .expect("resource must apply");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_identitical_pod_selector() {
    with_temp_ns(|client, ns| async move {
        let spec = ServerSpec {
            pod_selector: api::labels::Selector::from_iter(Some(("app", "test"))),
            port: Port::Number(80.try_into().unwrap()),
            proxy_protocol: None,
        };

        let api = kube::Api::namespaced(client, &*ns);

        let test0 = Server {
            metadata: api::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some("test0".to_string()),
                ..Default::default()
            },
            spec: spec.clone(),
        };
        api.create(&kube::api::PostParams::default(), &test0)
            .await
            .expect("resource must apply");

        let test1 = Server {
            metadata: api::ObjectMeta {
                namespace: Some(ns),
                name: Some("test1".to_string()),
                ..Default::default()
            },
            spec,
        };
        api.create(&kube::api::PostParams::default(), &test1)
            .await
            .expect_err("resource must not apply");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_all_pods_selected() {
    with_temp_ns(|client, ns| async move {
        let api = kube::Api::namespaced(client, &*ns);

        let test0 = Server {
            metadata: api::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some("test0".to_string()),
                ..Default::default()
            },
            spec: ServerSpec {
                pod_selector: api::labels::Selector::from_iter(Some(("app", "test"))),
                port: Port::Number(80.try_into().unwrap()),
                proxy_protocol: Some(ProxyProtocol::Http2),
            },
        };
        api.create(&kube::api::PostParams::default(), &test0)
            .await
            .expect("resource must apply");

        let test1 = Server {
            metadata: api::ObjectMeta {
                namespace: Some(ns),
                name: Some("test1".to_string()),
                ..Default::default()
            },
            spec: ServerSpec {
                pod_selector: api::labels::Selector::default(),
                port: Port::Number(80.try_into().unwrap()),
                // proxy protocol doesn't factor into the selection
                proxy_protocol: Some(ProxyProtocol::Http1),
            },
        };
        api.create(&kube::api::PostParams::default(), &test1)
            .await
            .expect_err("resource must not apply");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_invalid_proxy_protocol() {
    /// Define a Server resource with an invalid proxy protocol
    #[derive(
        Clone,
        Debug,
        kube::CustomResource,
        serde::Deserialize,
        serde::Serialize,
        schemars::JsonSchema,
    )]
    #[kube(
        group = "policy.linkerd.io",
        version = "v1alpha1",
        kind = "Server",
        namespaced
    )]
    #[serde(rename_all = "camelCase")]
    pub struct ServerSpec {
        pub pod_selector: labels::Selector,
        pub port: Port,
        pub proxy_protocol: String,
    }

    /// References a pod spec's port by name or number.
    #[derive(Clone, Debug, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
    #[serde(rename_all = "camelCase")]
    pub enum Port {
        Number(u16),
        Name(String),
    }

    admission::rejects(|ns| Server {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: ServerSpec {
            pod_selector: api::labels::Selector::default(),
            port: Port::Number(80.try_into().unwrap()),
            proxy_protocol: "garbanzo".to_string(),
        },
    })
    .await;
}
