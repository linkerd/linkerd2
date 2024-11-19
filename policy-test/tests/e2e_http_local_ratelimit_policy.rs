use k8s::policy::{NamespacedTargetRef, RateLimitPolicySpec};
use kube::api::LogParams;
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{HttpLocalRateLimitPolicy, Limit, LocalTargetRef, Override},
};
use linkerd_policy_test::{
    await_condition, await_service_account, create, create_ready_pod, endpoints_ready, web,
    with_temp_ns,
};
use maplit::{btreemap, convert_args};

#[tokio::test(flavor = "current_thread")]
/// Tests reaching the rate limit for:
/// - a client with a meshed identity
/// - a client with a meshed identity that has an override
/// - an unmeshed client
async fn ratelimit_identity_and_overrides() {
    with_temp_ns(|client, ns| async move {
        // create a server with a permissive access policy to not worry about auth
        create(
            &client,
            web::server(&ns, Some("all-unauthenticated".to_string())),
        )
        .await;

        // create the pod "web" with its service
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        mk_ratelimit(&client, &ns, Some(70), Some(5), Some(10)).await;

        tokio::join!(
            create_service_account(&client, &ns, "meshed-regular"),
            create_service_account(&client, &ns, "meshed-id-1"),
            create_service_account(&client, &ns, "unmeshed"),
        );

        // all clients will send 20rps for 30s
        tokio::join!(
            create_fortio_pod(&client, &ns, "meshed-regular", true, 20, 30),
            create_fortio_pod(&client, &ns, "meshed-id-1", true, 20, 30),
            create_fortio_pod(&client, &ns, "unmeshed", false, 20, 30),
        );

        let (rl_meshed_regular, rl_meshed_id_1, rl_unmeshed) = tokio::join!(
            fetch_fortio_rl(&client, &ns, "meshed-regular"),
            fetch_fortio_rl(&client, &ns, "meshed-id-1"),
            fetch_fortio_rl(&client, &ns, "unmeshed"),
        );
        tracing::info!(%rl_meshed_regular, %rl_meshed_id_1, %rl_unmeshed, "Rate limit percentages");

        // for 20rps rate-limited at 5rps, we expect around 75% of requests to be rate limited
        assert!(
            (70..=80).contains(&rl_meshed_regular),
            "around 75% of meshed-regular's requests should be rate limited",
        );

        // for 20rps rate-limited at 10rps, we expect around 50% of requests to be rate limited
        assert!(
            (45..=55).contains(&rl_meshed_id_1),
            "around 50% of meshed-id-1's requests should be rate limited",
        );

        // for 20rps rate-limited at 5rps, we expect around 75% of requests to be rate limited
        assert!(
            (70..=80).contains(&rl_unmeshed),
            "around 75% of unmeshed's requests should be rate limited",
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
/// Tests reaching the total rate limit
async fn ratelimit_total() {
    with_temp_ns(|client, ns| async move {
        // create a server with a permissive access policy to not worry about auth
        create(
            &client,
            web::server(&ns, Some("all-unauthenticated".to_string())),
        )
        .await;

        // create the pod "web" with its service
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        mk_ratelimit(&client, &ns, Some(70), None, None).await;

        create_service_account(&client, &ns, "meshed-id-1").await;

        // client will send 100rps for 30s
        create_fortio_pod(&client, &ns, "meshed-id-1", true, 100, 30).await;

        let rl_meshed_id_1 = fetch_fortio_rl(&client, &ns, "meshed-id-1").await;
        tracing::info!(%rl_meshed_id_1, "Rate limit percentage");

        // for 100rps rate-limited at 70rps, we expect around 30% of requests to be rate limited
        assert!(
            (25..=35).contains(&rl_meshed_id_1),
            "around 30% of meshed-id-1's requests should be rate limited",
        );
    })
    .await;
}

/// Makes a ratelimit policy "rl" with the given rates, where override_rps is the rate limit for
/// the meshed-id-1 service account. It waits for the ratelimit to be accepted by the policy
/// controller.
async fn mk_ratelimit(
    client: &kube::Client,
    ns: &str,
    total_rps: Option<u32>,
    identity_rps: Option<u32>,
    override_rps: Option<u32>,
) {
    let total = total_rps.map(|rps| Limit {
        requests_per_second: rps,
    });
    let identity = identity_rps.map(|rps| Limit {
        requests_per_second: rps,
    });
    let overrides = override_rps.map(|rps| {
        vec![Override {
            requests_per_second: rps,
            client_refs: vec![NamespacedTargetRef {
                group: Some("core".to_string()),
                kind: "ServiceAccount".to_string(),
                name: "meshed-id-1".to_string(),
                namespace: Some(ns.to_string()),
            }],
        }]
    });
    create(
        client,
        HttpLocalRateLimitPolicy {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("rl".to_string()),
                ..Default::default()
            },
            spec: RateLimitPolicySpec {
                target_ref: LocalTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "Server".to_string(),
                    name: "web".to_string(),
                },
                total,
                identity,
                overrides,
            },
            status: None,
        },
    )
    .await;

    tracing::debug!(%ns, "Waiting for ratelimit to be accepted");
    await_condition(client, ns, "rl", ratelimit_accepted).await;
}

// Wait for the ratelimit to be accepted by the policy controller
fn ratelimit_accepted(obj: Option<&HttpLocalRateLimitPolicy>) -> bool {
    if let Some(rl) = obj {
        return rl
            .status
            .as_ref()
            .map(|s| {
                s.conditions
                    .iter()
                    .any(|c| c.type_ == "Accepted" && c.status == "True")
            })
            .unwrap_or(false);
    }
    false
}

async fn create_service_account(client: &kube::Client, ns: &str, name: &str) {
    create(
        client,
        k8s::api::core::v1::ServiceAccount {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some(name.to_string()),
                ..Default::default()
            },
            ..Default::default()
        },
    )
    .await;
    await_service_account(client, ns, name).await;
}

// Creates a fortio pod that sends requests to the "web" service
async fn create_fortio_pod(
    client: &kube::Client,
    ns: &str,
    name: &str,
    injected: bool,
    rps: u32,
    duration: u32,
) {
    let annotations = if injected {
        Some(convert_args!(btreemap!(
           "linkerd.io/inject" => "enabled",
        )))
    } else {
        None
    };

    let pod = k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            annotations,
            labels: Some(convert_args!(btreemap!(
                "app" => name,
            ))),
            ..Default::default()
        },
        spec: Some(k8s::PodSpec {
            containers: vec![k8s::api::core::v1::Container {
                name: "fortio".to_string(),
                image: Some("fortio/fortio:latest".to_string()),
                args: Some(vec![
                    "load".to_string(),
                    "-qps".to_string(),
                    rps.to_string(),
                    "-t".to_string(),
                    format!("{}s", duration),
                    "-quiet".to_string(),
                    "http://web".to_string(),
                ]),
                ..Default::default()
            }],
            service_account_name: Some(name.to_string()),
            ..Default::default()
        }),
        ..k8s::Pod::default()
    };

    create_ready_pod(client, pod).await;
}

/// Waits for the fortio pod to complete and parses the logs to extract the rate limit percentage.
async fn fetch_fortio_rl(client: &kube::Client, ns: &str, name: &str) -> u32 {
    tracing::debug!(%ns, %name, "Waiting to finish");
    await_condition(client, ns, name, is_fortio_container_terminated).await;

    // log output should look something like "Code 429 : 454 (75.7 %)"
    let pattern = r"Code 429 : \d+ \((\d+)\.?\d+? %\)";
    let re = regex::Regex::new(pattern).unwrap();

    let log_params = LogParams {
        container: Some("fortio".to_string()),
        tail_lines: Some(2),
        ..Default::default()
    };
    let api = kube::Api::<k8s::Pod>::namespaced(client.clone(), ns);
    let result = api
        .logs(name, &log_params)
        .await
        .expect("failed to fetch logs")
        .split('\n')
        .take(1)
        .collect::<String>();

    re.captures(&result)
        .unwrap_or_else(|| panic!("failed to parse log: {result}"))
        .get(1)
        .unwrap_or_else(|| panic!("failed to parse log: {result}"))
        .as_str()
        .parse()
        .expect("failed to parse rate limit percentage")
}

fn is_fortio_container_terminated(pod: Option<&k8s::Pod>) -> bool {
    let terminated = || -> Option<&k8s_openapi::api::core::v1::ContainerStateTerminated> {
        pod?.status
            .as_ref()?
            .container_statuses
            .as_ref()?
            .iter()
            .find(|cs| cs.name == "fortio")?
            .state
            .as_ref()?
            .terminated
            .as_ref()
    };
    terminated().is_some()
}
