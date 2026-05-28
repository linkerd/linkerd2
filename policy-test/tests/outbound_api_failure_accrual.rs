use std::time::Duration;

use futures::StreamExt;
use kube::Resource;
use linkerd_policy_controller_k8s_api::{self as k8s, policy};
use linkerd_policy_test::{
    assert_default_accrual_backoff, assert_resource_meta, create, grpc,
    outbound_api::{
        assert_load_eq, detect_failure_accrual, failure_accrual_consecutive, penalty_peak_ewma, peak_ewma, retry_watch_outbound_policy
    },
    test_route::TestParent,
    update, with_temp_ns,
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
                        respect_retry_after_hint: false,
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
                        respect_retry_after_hint: false,
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

#[tokio::test(flavor = "current_thread")]
async fn load_bias_with_custom_penalty() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/load-bias".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/load-bias-penalty".to_string() => "3s".to_string(),
                "balancer.alpha.linkerd.io/load-bias-penalty-decay".to_string() => "6s".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            let dt = P::DynamicType::default();
            if P::kind(&dt) == "EgressNetwork" {
                assert_load_eq(&config, None);
            } else {
                assert_load_eq(&config, Some(penalty_peak_ewma(
                    Some(Duration::from_secs(3)),
                    Some(Duration::from_secs(6)),
                    None,
                )));
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn retry_after_with_custom_max() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/retry-after".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/retry-after-max-duration".to_string() => "120s".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            let dt = P::DynamicType::default();
            if P::kind(&dt) == "EgressNetwork" {
                assert_load_eq(&config, None);
            } else {
                assert_load_eq(&config, Some(penalty_peak_ewma(None, None, Some(Duration::from_secs(120)))));
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn combined_load_bias_and_retry_after() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/load-bias".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/retry-after".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            let dt = P::DynamicType::default();
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            if P::kind(&dt) == "EgressNetwork" {
                assert!(
                    load_bias.is_none(),
                    "EgressNetwork should not have load_bias"
                );
                assert!(
                    retry_after.is_none(),
                    "EgressNetwork should not have retry_after"
                );
            } else {
                let lb = load_bias.expect("load_bias must be configured");
                assert!(lb.enabled, "load_bias must be enabled");
                assert_eq!(
                    lb.penalty,
                    Some(Duration::from_secs(5).try_into().unwrap()),
                    "default penalty should be 5s"
                );
                assert_eq!(
                    lb.penalty_decay,
                    Some(Duration::from_secs(10).try_into().unwrap()),
                    "default penalty_decay should be 10s"
                );

                let ra = retry_after.expect("retry_after must be configured");
                assert_eq!(
                    ra.max_duration,
                    Some(Duration::from_secs(300).try_into().unwrap()),
                    "default max_duration should be 300s"
                );
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn accrual_with_load_bias_and_retry_after() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                "balancer.alpha.linkerd.io/load-bias".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/retry-after".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            detect_failure_accrual(&config, |accrual| {
                let _consecutive = failure_accrual_consecutive(accrual);
            });

            let dt = P::DynamicType::default();
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            if P::kind(&dt) == "EgressNetwork" {
                assert!(
                    load_bias.is_none(),
                    "EgressNetwork should not have load_bias"
                );
                assert!(
                    retry_after.is_none(),
                    "EgressNetwork should not have retry_after"
                );
            } else {
                let lb = load_bias.expect("load_bias must be configured");
                assert!(lb.enabled, "load_bias must be enabled");
                assert_eq!(
                    lb.penalty,
                    Some(Duration::from_secs(5).try_into().unwrap()),
                    "default penalty should be 5s"
                );
                assert_eq!(
                    lb.penalty_decay,
                    Some(Duration::from_secs(10).try_into().unwrap()),
                    "default penalty_decay should be 10s"
                );

                let ra = retry_after.expect("retry_after must be configured");
                assert_eq!(
                    ra.max_duration,
                    Some(Duration::from_secs(300).try_into().unwrap()),
                    "default max_duration should be 300s"
                );
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn invalid_load_bias_mode_produces_default() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/load-bias".to_string() => "invalid".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // Invalid mode value causes a parse error. The indexer logs
            // a warning and falls through to the default (no load bias).
            let (load_bias, _) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "invalid load-bias mode should produce no load_bias config"
            );
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn invalid_retry_after_duration_produces_default() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/retry-after".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/retry-after-max-duration".to_string() => "5".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // Bare number "5" lacks a duration unit, causing a parse error.
            // The indexer logs a warning and falls through to the default.
            let (_, retry_after) = detect_load_bias_and_retry_after(&config);
            assert!(
                retry_after.is_none(),
                "bare-number duration should produce no retry_after config"
            );
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn unannotated_service_has_no_new_config() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let parent = P::make_parent(&ns);
            // No balancer annotations at all -- backwards compatibility test.
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // No annotations means no failure accrual, load bias, or retry
            // after config -- zero behavior change for unannotated resources.
            detect_failure_accrual(&config, |accrual| {
                assert!(
                    accrual.is_none(),
                    "unannotated resource should have no failure accrual"
                );
            });
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "unannotated resource should have no load_bias"
            );
            assert!(
                retry_after.is_none(),
                "unannotated resource should have no retry_after"
            );
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_network_ignores_load_bias_and_retry_after() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/load-bias".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/load-bias-penalty".to_string() => "3s".to_string(),
                "balancer.alpha.linkerd.io/retry-after".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/retry-after-max-duration".to_string() => "60s".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // EgressNetworks use Forward instead of Balancer, so the
            // indexer skips load-bias and retry-after even when annotated.
            let dt = P::DynamicType::default();
            let kind = P::kind(&dt);
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "{kind} should not have load_bias"
            );
            assert!(
                retry_after.is_none(),
                "{kind} should not have retry_after"
            );
        })
        .await;
    }

    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn consecutive_accrual_pipeline_unchanged() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
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

            // Consecutive mode should produce failure accrual with
            // consecutive_failures but no success_rate.
            detect_failure_accrual(&config, |accrual| {
                let accrual = accrual.expect("failure accrual must be configured");
                assert!(
                    accrual.consecutive_failures.is_some(),
                    "consecutive_failures must be present"
                );
                assert!(
                    accrual.success_rate.is_none(),
                    "success_rate must NOT be set in consecutive mode"
                );
            });

            // Consecutive mode should not enable load bias or retry after.
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "consecutive mode should not enable load_bias"
            );
            assert!(
                retry_after.is_none(),
                "consecutive mode should not enable retry_after"
            );
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn load_bias_watch_update() {
    with_temp_ns(|client, ns| async move {
        let port = 4191;
        let parent = k8s::Service::make_parent(&ns);
        let mut parent = create(&client, parent).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_load_eq(&config, peak_ewma());

        parent.meta_mut().annotations = Some(btreemap! {
            "balancer.alpha.linkerd.io/load-bias".to_string() => "true".to_string(),
            "balancer.alpha.linkerd.io/load-bias-penalty".to_string() => "3s".to_string(),
            "balancer.alpha.linkerd.io/retry-after".to_string() => "true".to_string(),
            "balancer.alpha.linkerd.io/retry-after-max-duration".to_string() => "60s".to_string(),
        });
        update(&client, parent).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
        let lb = load_bias.expect("load_bias must be present after update");
        assert!(lb.enabled, "load_bias must be enabled");
        assert_eq!(
            lb.penalty,
            Some(Duration::from_secs(3).try_into().unwrap()),
            "penalty should be 3s"
        );

        let ra = retry_after.expect("retry_after must be present after update");
        assert_eq!(
            ra.max_duration,
            Some(Duration::from_secs(60).try_into().unwrap()),
            "max_duration should be 60s"
        );
    })
    .await;
}
