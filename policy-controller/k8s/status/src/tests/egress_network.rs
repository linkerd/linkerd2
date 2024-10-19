use crate::{
    index::{accepted, in_cluster_net_overlap},
    resource_id::NamespaceGroupKindName,
    tests::default_cluster_networks,
    Index, IndexMetrics,
};
use chrono::{DateTime, Utc};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::routes::GroupKindName;
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api,
    policy::{self as linkerd_k8s_api, EgressNetworkStatus},
    Resource,
};
use std::{sync::Arc, vec};
use tokio::sync::{mpsc, watch};

#[test]
fn egress_network_with_no_networks_specified() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
        default_cluster_networks(),
    );

    let id = NamespaceGroupKindName {
        namespace: "ns".to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::EgressNetwork::group(&()),
            kind: linkerd_k8s_api::EgressNetwork::kind(&()),
            name: "egress".into(),
        },
    };

    let egress_network = linkerd_k8s_api::EgressNetwork {
        metadata: k8s_core_api::ObjectMeta {
            name: Some(id.gkn.name.to_string()),
            namespace: Some(id.namespace.clone()),
            ..Default::default()
        },
        spec: linkerd_k8s_api::EgressNetworkSpec {
            networks: None,
            traffic_policy: linkerd_k8s_api::TrafficPolicy::Allow,
        },
        status: None,
    };

    index.write().apply(egress_network.clone());

    // Create the expected update.
    let status = EgressNetworkStatus {
        conditions: vec![accepted()],
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn egress_network_with_nonoverlapping_networks_specified() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
        default_cluster_networks(),
    );

    let id = NamespaceGroupKindName {
        namespace: "ns".to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::EgressNetwork::group(&()),
            kind: linkerd_k8s_api::EgressNetwork::kind(&()),
            name: "egress".into(),
        },
    };

    let egress_network = linkerd_k8s_api::EgressNetwork {
        metadata: k8s_core_api::ObjectMeta {
            name: Some(id.gkn.name.to_string()),
            namespace: Some(id.namespace.clone()),
            ..Default::default()
        },
        spec: linkerd_k8s_api::EgressNetworkSpec {
            networks: Some(vec![linkerd_k8s_api::Network {
                cidr: "0.0.0.0/0".parse().unwrap(),
                except: Some(vec![
                    "10.0.0.0/8".parse().unwrap(),
                    "100.64.0.0/10".parse().unwrap(),
                    "172.16.0.0/12".parse().unwrap(),
                    "192.168.0.0/16".parse().unwrap(),
                    "fd00::/8".parse().unwrap(),
                ]),
            }]),
            traffic_policy: linkerd_k8s_api::TrafficPolicy::Allow,
        },
        status: None,
    };

    index.write().apply(egress_network.clone());

    // Create the expected update.
    let status = EgressNetworkStatus {
        conditions: vec![accepted()],
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn egress_network_with_overlapping_networks_specified() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
        default_cluster_networks(),
    );

    let id = NamespaceGroupKindName {
        namespace: "ns".to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::EgressNetwork::group(&()),
            kind: linkerd_k8s_api::EgressNetwork::kind(&()),
            name: "egress".into(),
        },
    };

    let egress_network = linkerd_k8s_api::EgressNetwork {
        metadata: k8s_core_api::ObjectMeta {
            name: Some(id.gkn.name.to_string()),
            namespace: Some(id.namespace.clone()),
            ..Default::default()
        },
        spec: linkerd_k8s_api::EgressNetworkSpec {
            networks: Some(vec![linkerd_k8s_api::Network {
                cidr: "0.0.0.0/0".parse().unwrap(),
                except: Some(vec![
                    "10.0.0.0/8".parse().unwrap(),
                    "100.64.0.0/10".parse().unwrap(),
                    "192.168.0.0/16".parse().unwrap(),
                ]),
            }]),
            traffic_policy: linkerd_k8s_api::TrafficPolicy::Allow,
        },
        status: None,
    };

    index.write().apply(egress_network.clone());

    // Create the expected update.
    let status = EgressNetworkStatus {
        conditions: vec![in_cluster_net_overlap()],
    };
    let patch = crate::index::make_patch(&id, status).unwrap();
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}
