use linkerd_policy_test::{
    annotate_service, bb, create, create_ready_pod, curl, with_temp_ns, LinkerdInject,
};

#[tokio::test(flavor = "current_thread")]
async fn consecutive_failures() {
    const MAX_FAILS: usize = 5;
    with_temp_ns(|client, ns| async move {
        // Create a bb service with one pod that always returns 500s.
        let bad_pod = bb::Terminus::new(&ns)
            .named("bb-bad")
            .percent_failure(100)
            .to_pod();
        let svc = {
            let svc = bb::Terminus::service(&ns);
            annotate_service(svc, maplit::btreemap!{
                "balancer.linkerd.io/failure-accrual" => "consecutive".to_string(),
                "balancer.linkerd.io/failure-accrual-consecutive-max-failures" => MAX_FAILS.to_string(),
                // don't allow the failing pod to enter probation during the test.
                "balancer.linkerd.io/failure-accrual-consecutive-min-penalty" => "5m".to_string(),
                "balancer.linkerd.io/failure-accrual-consecutive-max-penalty" => "10m".to_string(),
            })
        };
        tokio::join!(
            create(&client, svc),
            create_ready_pod(&client, bad_pod),
        );

        let curl = curl::Runner::init(&client, &ns)
            .await
            .run_execable("curl", LinkerdInject::Enabled)
            .await;

        let url = format!("http://{}", bb::Terminus::SERVICE_NAME);
        for request in 0..MAX_FAILS * 2 {
            tracing::info!("Sending request {request}...");
            let status = curl
                .get(&url)
                .await
                .expect("curl command should succeed");
            tracing::info!(request, ?status);
            if request < MAX_FAILS {
                assert_eq!(status, hyper::StatusCode::INTERNAL_SERVER_ERROR);
            } else {
                // Once the circuit breaker has tripped, any in flight request
                // will fail with a 504 due to failfast, and subsequent requests
                // will fail with a 503 because the balancer has no available
                // endpoints.
                assert!(
                    matches!(status, hyper::StatusCode::GATEWAY_TIMEOUT | hyper::StatusCode::SERVICE_UNAVAILABLE),
                    "expected 503 or 504, got {status:?}"
                );
            }
        }
    })
    .await;
}
