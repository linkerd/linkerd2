use linkerd_policy_test::{
    annotate_service, bb, create, create_ready_pod, curl, with_temp_ns, LinkerdInject,
};

#[tokio::test(flavor = "current_thread")]
async fn consecutive_failures() {
    const MAX_FAILS: usize = 5;
    const REQUESTS: usize = 20;
    with_temp_ns(|client, ns| async move {
        // Create a bb service with two pods, one of which always returns 200
        // OK, and the other of which always returns 500 Internal Server Error.
        let good_pod = bb::Terminus::new(&ns)
            .named("bb-good")
            .percent_failure(0)
            .to_pod();
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
            create_ready_pod(&client, good_pod),
            create_ready_pod(&client, bad_pod),
        );

        let curl = curl::Runner::init(&client, &ns)
            .await
            .run_execable("curl", LinkerdInject::Enabled)
            .await;

        let url = format!("http://{}", bb::Terminus::SERVICE_NAME);
        let mut failures = 0;

        for request in 0..REQUESTS {
            tracing::info!("Sending request {request}...");
            let status = curl
                .get(&url)
                .await
                .expect("curl command should succeed");
            tracing::info!(request, ?status, failures);

            match status {
                // An error was returned by the failing endpoint.
                hyper::StatusCode::INTERNAL_SERVER_ERROR => failures += 1,
                hyper::StatusCode::OK => {},
                other => panic!("unexpected status code {other}"),
            }
        }

        assert!(failures <= MAX_FAILS, "no more than {MAX_FAILS} requests may fail");
    })
    .await;
}
