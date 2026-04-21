use super::*;

/// Tests that pod servers are configured with defaults based on the
/// workload-defined `DefaultPolicy` policy.
///
/// Iterates through each default policy and validates that it produces expected
/// configurations.
#[test]
fn default_policy_annotated() {
    for default in &DEFAULTS {
        let test = TestConfig::from_default_policy(match *default {
            // Invert default to ensure override applies.
            DefaultPolicy::Deny => DefaultPolicy::Allow {
                authenticated_only: false,
                cluster_only: false,
            },
            _ => DefaultPolicy::Deny,
        });

        // Initially create the pod without an annotation and check that it gets
        // the global default.
        let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
        test.index
            .write()
            .reset(vec![pod.clone()], Default::default());

        let mut rx = test
            .index
            .write()
            .pod_server_rx("ns-0", "pod-0", 2222.try_into().unwrap())
            .expect("pod-0.ns-0 should exist");
        assert_eq!(
            rx.borrow_and_update().reference,
            ServerRef::Default(test.default_policy.as_str()),
        );

        // Update the annotation on the pod and check that the watch is updated
        // with the new default.
        pod.annotations_mut().insert(
            "config.linkerd.io/default-inbound-policy".into(),
            default.to_string(),
        );
        test.index.write().apply(pod);
        assert!(rx.has_changed().unwrap());
        assert_eq!(rx.borrow().reference, ServerRef::Default(default.as_str()));
    }
}

/// Tests that an invalid workload annotation is ignored in favor of the global
/// default.
#[tokio::test]
async fn default_policy_annotated_invalid() {
    let test = TestConfig::default();

    let mut p = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    p.annotations_mut().insert(
        "config.linkerd.io/default-inbound-policy".into(),
        "bogus".into(),
    );
    test.index.write().reset(vec![p], Default::default());

    // Lookup port 2222 -> default config.
    let rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 2222.try_into().unwrap())
        .expect("pod must exist in lookups");
    assert_eq!(*rx.borrow(), test.default_server());
}

#[test]
fn opaque_annotated() {
    for default in &DEFAULTS {
        let test = TestConfig::from_default_policy(*default);

        let mut p = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
        p.annotations_mut()
            .insert("config.linkerd.io/opaque-ports".into(), "2222".into());
        test.index.write().reset(vec![p], Default::default());

        let mut server = test.default_server();
        server.protocol = ProxyProtocol::Opaque;

        let rx = test
            .index
            .write()
            .pod_server_rx("ns-0", "pod-0", 2222.try_into().unwrap())
            .expect("pod-0.ns-0 should exist");
        assert_eq!(*rx.borrow(), server);
    }
}

#[test]
fn authenticated_annotated() {
    for default in &DEFAULTS {
        let test = TestConfig::from_default_policy(*default);

        let mut p = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
        p.annotations_mut().insert(
            "config.linkerd.io/proxy-require-identity-inbound-ports".into(),
            "2222".into(),
        );
        test.index.write().reset(vec![p], Default::default());

        let config = {
            let policy = match *default {
                DefaultPolicy::Allow { cluster_only, .. } => DefaultPolicy::Allow {
                    cluster_only,
                    authenticated_only: true,
                },
                DefaultPolicy::Deny => DefaultPolicy::Deny,
                DefaultPolicy::Audit => DefaultPolicy::Audit,
            };
            InboundServer {
                reference: ServerRef::Default(policy.as_str()),
                authorizations: mk_default_policy(policy, test.cluster.networks),
                ratelimit: None,
                protocol: ProxyProtocol::Detect {
                    timeout: test.detect_timeout,
                },
                http_routes: mk_default_http_routes(),
                grpc_routes: Default::default(),
            }
        };

        let rx = test
            .index
            .write()
            .pod_server_rx("ns-0", "pod-0", 2222.try_into().unwrap())
            .expect("pod-0.ns-0 should exist");
        assert_eq!(*rx.borrow(), config);
    }
}
