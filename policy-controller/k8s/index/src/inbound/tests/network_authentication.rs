use super::*;
use ahash::AHashSet;

// === reset ===

#[test]
fn reset_network_authn_with_deleted_entries() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);
    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    test.index.write().apply(mk_authorization_policy(
        "ns-0",
        "authz-policy-0",
        Some("srv-8080"),
        vec![NamespacedTargetRef {
            group: Some("policy.linkerd.io".to_string()),
            kind: "NetworkAuthentication".to_string(),
            name: "network-authn-0".to_string(),
            namespace: None,
        }],
    ));
    test.index.write().apply(mk_network_authentication(
        "ns-0",
        "network-authn-0",
        vec![k8s::policy::network_authentication::Network {
            cidr: "10.0.0.0/8".parse().unwrap(),
            except: None,
        }],
    ));

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert!(
        rx.borrow_and_update()
            .authorizations
            .contains_key(&AuthorizationRef::AuthorizationPolicy(
                "authz-policy-0".to_string()
            )),
        "authz-foo should be present before reset"
    );

    // reset and delete network authentication policy
    let mut deleted: HashMap<String, AHashSet<String>> = HashMap::default();
    deleted.insert(
        "ns-0".to_string(),
        AHashSet::from_iter(["network-authn-0".to_string()]),
    );
    <Index as IndexNamespacedResource<k8s::policy::NetworkAuthentication>>::reset(
        &mut test.index.write(),
        vec![],
        deleted,
    );

    assert!(rx.has_changed().unwrap());
    assert!(
        !rx.borrow_and_update()
            .authorizations
            .contains_key(&AuthorizationRef::AuthorizationPolicy(
                "authz-policy-0".to_string()
            )),
        "authz-policy-0 should be absent after reset removes it"
    );
}
