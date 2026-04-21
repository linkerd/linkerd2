use std::time::Duration;

use futures::StreamExt;
use linkerd_policy_controller_k8s_api::{self as k8s, policy};
use linkerd_policy_test::{
    assert_default_accrual_backoff, assert_resource_meta, create, grpc,
    outbound_api::{
        detect_failure_accrual, failure_accrual_consecutive, retry_watch_outbound_policy,
    },
    test_route::TestParent,
    with_temp_ns,
};
use maplit::btreemap;

#[tokio::test(flavor = "current_thread")]
async fn consecutive_failure_accrual() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                "balancer.linkerd.io/failure-accrual-consecutive-max-failures".to_string() => "8".to_string(),
                "balancer.linkerd.io/failure-accrual-consecutive-min-penalty".to_string() => "10s".to_string(),
                "balancer.linkerd.io/failure-accrual-consecutive-max-penalty".to_string() => "10m".to_string(),
                "balancer.linkerd.io/failure-accrual-consecutive-jitter-ratio".to_string() => "1.0".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            detect_failure_accrual(&config, |accrual| {
                let consecutive = failure_accrual_consecutive(accrual);
                assert_eq!(8, consecutive.max_failures);
                assert_eq!(
                    &grpc::outbound::ExponentialBackoff {
                        min_backoff: Some(Duration::from_secs(10).try_into().unwrap()),
                        max_backoff: Some(Duration::from_secs(600).try_into().unwrap()),
                        jitter_ratio: 1.0_f32,
                    },
                    consecutive
                        .backoff
                        .as_ref()
                        .expect("backoff must be configured")
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn consecutive_failure_accrual_defaults_no_config() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a service configured to do consecutive failure accrual, but
            // with no additional configuration
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // Expect default max_failures and default backoff
            detect_failure_accrual(&config, |accrual| {
                let consecutive = failure_accrual_consecutive(accrual);
                assert_eq!(7, consecutive.max_failures);
                assert_default_accrual_backoff!(consecutive
                    .backoff
                    .as_ref()
                    .expect("backoff must be configured"));
            });
        })
        .await
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn consecutive_failure_accrual_defaults_max_fails() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a service configured to do consecutive failure accrual with
            // max number of failures and with default backoff
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                "balancer.linkerd.io/failure-accrual-consecutive-max-failures".to_string() => "8".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // Expect default backoff and overridden max_failures
            detect_failure_accrual(&config, |accrual| {
                let consecutive = failure_accrual_consecutive(accrual);
                assert_eq!(8, consecutive.max_failures);
                assert_default_accrual_backoff!(consecutive
                    .backoff
                    .as_ref()
                    .expect("backoff must be configured"));
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn consecutive_failure_accrual_defaults_jitter() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a service configured to do consecutive failure accrual with
            // max number of failures and with default backoff
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                    "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                    "balancer.linkerd.io/failure-accrual-consecutive-jitter-ratio".to_string() => "1.0".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // Expect defaults for everything except for the jitter ratio
            detect_failure_accrual(&config, |accrual| {
                let consecutive = failure_accrual_consecutive(accrual);
                assert_eq!(7, consecutive.max_failures);
                assert_eq!(
                    &grpc::outbound::ExponentialBackoff {
                        min_backoff: Some(Duration::from_secs(1).try_into().unwrap()),
                        max_backoff: Some(Duration::from_secs(60).try_into().unwrap()),
                        jitter_ratio: 1.0_f32,
                    },
                    consecutive
                        .backoff
                        .as_ref()
                        .expect("backoff must be configured")
                );
            });
        })
    .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn default_failure_accrual() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create Service with consecutive failure accrual config for
            // max_failures but no mode
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual-consecutive-max-failures".to_string() => "8".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // Expect failure accrual config to be default (no failure accrual)
            detect_failure_accrual(&config, |accrual| {
                assert!(
                    accrual.is_none(),
                    "consecutive failure accrual should not be configured for service"
                );
            });
        })
    .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}
