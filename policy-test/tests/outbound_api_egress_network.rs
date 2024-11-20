use futures::prelude::*;
use linkerd2_proxy_api::meta;
use linkerd_policy_test::{
    await_egress_net_status, create_egress_network, delete, grpc, with_temp_ns,
};

#[tokio::test(flavor = "current_thread")]
async fn egress_switches_to_fallback() {
    with_temp_ns(|client, ns| async move {
        let egress_net = create_egress_network(&client, &ns, "egress-net").await;
        await_egress_net_status(&client, &ns, "egress-net").await;

        let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(&client).await;
        let mut rsp = policy_api.watch_ip(&ns, "1.1.1.1", 80).await.unwrap();

        let policy = rsp.next().await.unwrap().unwrap();
        let meta = policy.metadata.unwrap();

        let expected_meta = meta::Metadata {
            kind: Some(meta::metadata::Kind::Resource(meta::Resource {
                group: "policy.linkerd.io".to_string(),
                port: 80,
                kind: "EgressNetwork".to_string(),
                name: "egress-net".to_string(),
                namespace: ns.clone(),
                section: "".to_string(),
            })),
        };

        assert_eq!(meta, expected_meta);

        delete(&client, egress_net).await;
        assert!(rsp.next().await.is_none());

        let mut rsp = policy_api.watch_ip(&ns, "1.1.1.1", 80).await.unwrap();

        let policy = rsp.next().await.unwrap().unwrap();
        let meta = policy.metadata.unwrap();
        let expected_meta = meta::Metadata {
            kind: Some(meta::metadata::Kind::Default("egress-fallback".to_string())),
        };
        assert_eq!(meta, expected_meta);
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn fallback_switches_to_egress() {
    with_temp_ns(|client, ns| async move {
        let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(&client).await;
        let mut rsp = policy_api.watch_ip(&ns, "1.1.1.1", 80).await.unwrap();

        let policy = rsp.next().await.unwrap().unwrap();
        let meta = policy.metadata.unwrap();
        let expected_meta = meta::Metadata {
            kind: Some(meta::metadata::Kind::Default("egress-fallback".to_string())),
        };
        assert_eq!(meta, expected_meta);

        let _egress_net = create_egress_network(&client, &ns, "egress-net").await;
        await_egress_net_status(&client, &ns, "egress-net").await;

        // stream should fall apart now
        assert!(rsp.next().await.is_none());

        // we should switch to an egress net now
        let mut rsp = policy_api.watch_ip(&ns, "1.1.1.1", 80).await.unwrap();
        let policy = rsp.next().await.unwrap().unwrap();
        let meta = policy.metadata.unwrap();

        let expected_meta = meta::Metadata {
            kind: Some(meta::metadata::Kind::Resource(meta::Resource {
                group: "policy.linkerd.io".to_string(),
                port: 80,
                kind: "EgressNetwork".to_string(),
                name: "egress-net".to_string(),
                namespace: ns.clone(),
                section: "".to_string(),
            })),
        };

        assert_eq!(meta, expected_meta);
    })
    .await;
}
