use std::time::Duration;

use futures::StreamExt;
use kube::Resource;
use linkerd_policy_controller_k8s_api::{self as k8s, policy};
use linkerd_policy_test::{
    assert_default_accrual_backoff, assert_resource_meta, create, grpc,
    outbound_api::{
        assert_default_penalty_estimator, assert_default_retry_after_cap, detect_failure_accrual,
        detect_load_bias_and_retry_after, failure_accrual_consecutive, failure_accrual_unified,
        opaque_default_backend_load_bias_and_retry_after, retry_watch_outbound_policy,
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
async fn unified_failure_accrual() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent configured to do unified failure accrual, with
            // explicit values for every parameter
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "unified".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold".to_string() => "0.9".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-window".to_string() => "30s".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests".to_string() => "100".to_string(),
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
                let unified = failure_accrual_unified(accrual);
                assert!((unified.success_rate_threshold - 0.9).abs() < f64::EPSILON);
                assert_eq!(
                    unified.decay,
                    Some(Duration::from_secs(30).try_into().unwrap())
                );
                assert_eq!(100, unified.min_requests);
                assert_eq!(8, unified.max_consecutive_failures);
                assert_eq!(
                    &grpc::outbound::ExponentialBackoff {
                        min_backoff: Some(Duration::from_secs(10).try_into().unwrap()),
                        max_backoff: Some(Duration::from_secs(600).try_into().unwrap()),
                        jitter_ratio: 1.0_f32,
                        respect_retry_after_hint: false,
                    },
                    unified
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
async fn unified_failure_accrual_defaults_no_config() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent configured to do unified failure accrual with
            // no additional configuration
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "unified".to_string(),
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

            // Expect default values for all five Unified fields. The default
            // backoff leaves respect_retry_after_hint=false in the absence of
            // honor-retry-after.
            detect_failure_accrual(&config, |accrual| {
                let unified = failure_accrual_unified(accrual);
                assert!((unified.success_rate_threshold - 0.8).abs() < f64::EPSILON);
                assert_eq!(
                    unified.decay,
                    Some(Duration::from_secs(10).try_into().unwrap())
                );
                assert_eq!(5, unified.min_requests);
                assert_eq!(7, unified.max_consecutive_failures);
                assert_default_accrual_backoff!(unified
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
async fn unified_honor_retry_after_sets_hint() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            // The Retry-After opt-in is attached to the accrual backoff for
            // both accrual kinds and must apply to the unified case too.
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "unified".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // Honoring Retry-After sets the hint on the unified backoff.
            detect_failure_accrual(&config, |accrual| {
                let unified = failure_accrual_unified(accrual);
                let backoff = unified
                    .backoff
                    .as_ref()
                    .expect("backoff must be configured");
                assert!(
                    backoff.respect_retry_after_hint,
                    "honor-retry-after must set respect_retry_after_hint"
                );
            });

            // It does not switch the estimator. The load estimator stays plain
            // PeakEwma and reports no penalty or cap.
            let dt = P::DynamicType::default();
            if P::kind(&dt) != "EgressNetwork" {
                let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
                assert!(
                    load_bias.is_none(),
                    "honor-retry-after alone must keep plain PeakEwma"
                );
                assert!(
                    retry_after.is_none(),
                    "honor-retry-after alone must not carry a cap on the estimator"
                );
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn success_rate_annotations_ignored_under_consecutive() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            // mode=consecutive combined with a success-rate annotation keeps the
            // consecutive breaker. Success-rate annotations have no effect under
            // consecutive mode. The indexer warns and keeps the breaker.
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold".to_string() => "0.9".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // The consecutive breaker reaches the wire. The stray success-rate
            // annotation is inert rather than dropping the whole config.
            detect_failure_accrual(&config, |accrual| {
                let accrual = accrual.expect("failure accrual must be configured");
                assert!(
                    matches!(
                        accrual.kind,
                        Some(grpc::outbound::failure_accrual::Kind::ConsecutiveFailures(
                            _
                        ))
                    ),
                    "failure accrual kind must be consecutive failures, got:\n{:#?}",
                    accrual.kind
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn penalize_failures_emits_penalty_estimator() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
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
                    "EgressNetwork should not have a penalty estimator"
                );
            } else {
                let lb = load_bias.expect("penalty estimator must be configured");
                assert_default_penalty_estimator(&lb);
                // The biaser's Retry-After cap belongs to the penalty estimator.
                // So penalize-failures alone emits the 300s default cap,
                // regardless of honor-retry-after.
                let ra = retry_after.expect("penalty estimator holds the Retry-After cap");
                assert_default_retry_after_cap(&ra);
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn opaque_service_keeps_plain_estimator_on_default_backend() {
    with_temp_ns(|client, ns| async move {
        let port = 4191;
        // An explicitly opaque service handles non-HTTP traffic, where the
        // response-code penalty does not apply, so its default backend stays on
        // the plain PeakEwma estimator even with penalize-failures set.
        let mut parent =
            k8s::Service::make_parent_with_protocol(&ns, Some("linkerd.io/opaque".to_string()));
        parent.meta_mut().annotations = Some(btreemap! {
            "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
        });
        let parent = create(&client, parent).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // penalize-failures is a no-op for opaque traffic, so the opaque
        // default backend reports no penalty estimator.
        let (load_bias, retry_after) = opaque_default_backend_load_bias_and_retry_after(&config);
        assert!(
            load_bias.is_none(),
            "opaque service default backend must keep plain PeakEwma"
        );
        assert!(
            retry_after.is_none(),
            "opaque service default backend must not carry a Retry-After cap"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn honor_retry_after_keeps_plain_estimator() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            // A failure-accrual backoff must exist for respect_retry_after_hint
            // to have somewhere to land.
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // The consecutive breaker leaves the hint unset on its backoff even
            // when honor-retry-after is on.
            detect_failure_accrual(&config, |accrual| {
                let consecutive = failure_accrual_consecutive(accrual);
                let backoff = consecutive
                    .backoff
                    .as_ref()
                    .expect("backoff must be configured");
                assert!(
                    !backoff.respect_retry_after_hint,
                    "consecutive accrual must not set respect_retry_after_hint"
                );
            });

            // It does not switch the estimator. The load estimator stays plain
            // PeakEwma, so no penalty or cap is reported.
            let dt = P::DynamicType::default();
            if P::kind(&dt) != "EgressNetwork" {
                let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
                assert!(
                    load_bias.is_none(),
                    "honor-retry-after alone must keep plain PeakEwma"
                );
                assert!(
                    retry_after.is_none(),
                    "honor-retry-after alone must not carry a cap on the estimator"
                );
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn penalize_and_honor_retry_after_combined() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // The consecutive breaker leaves the hint unset on its backoff.
            detect_failure_accrual(&config, |accrual| {
                let consecutive = failure_accrual_consecutive(accrual);
                let backoff = consecutive
                    .backoff
                    .as_ref()
                    .expect("backoff must be configured");
                assert!(
                    !backoff.respect_retry_after_hint,
                    "consecutive accrual must not set respect_retry_after_hint"
                );
            });

            // The penalty estimator is emitted and holds the cap.
            let dt = P::DynamicType::default();
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            if P::kind(&dt) == "EgressNetwork" {
                assert!(
                    load_bias.is_none(),
                    "EgressNetwork should not have a penalty estimator"
                );
                assert!(
                    retry_after.is_none(),
                    "EgressNetwork should not carry a cap"
                );
            } else {
                let lb = load_bias.expect("penalty estimator must be configured");
                assert_default_penalty_estimator(&lb);

                let ra = retry_after.expect("Retry-After cap must be configured");
                assert_default_retry_after_cap(&ra);
            }
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn invalid_penalize_failures_value_produces_default() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/penalize-failures".to_string() => "invalid".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // An unrecognized value causes a parse error. The indexer logs a
            // warning and falls through to the default (plain PeakEwma).
            let (load_bias, _) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "invalid penalize-failures value should keep plain PeakEwma"
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
            // No balancer annotations at all. Backwards compatibility test.
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // No annotations means no failure accrual, penalty estimator, or
            // Retry-After cap. Zero behavior change for unannotated resources.
            detect_failure_accrual(&config, |accrual| {
                assert!(
                    accrual.is_none(),
                    "unannotated resource should have no failure accrual"
                );
            });
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "unannotated resource should keep plain PeakEwma"
            );
            assert!(
                retry_after.is_none(),
                "unannotated resource should have no Retry-After cap"
            );
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_network_ignores_balancer_toggles() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            // EgressNetworks use Forward instead of Balancer, so the indexer
            // emits no penalty estimator even when the toggles are set.
            let dt = P::DynamicType::default();
            let kind = P::kind(&dt);
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "{kind} should not have a penalty estimator"
            );
            assert!(
                retry_after.is_none(),
                "{kind} should not carry a Retry-After cap"
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

            // Consecutive mode should produce failure accrual whose kind is
            // consecutive-failures rather than the unified policy.
            detect_failure_accrual(&config, |accrual| {
                let accrual = accrual.expect("failure accrual must be configured");
                assert!(
                    matches!(
                        accrual.kind,
                        Some(grpc::outbound::failure_accrual::Kind::ConsecutiveFailures(
                            _
                        ))
                    ),
                    "failure accrual kind must be consecutive failures, got:\n{:#?}",
                    accrual.kind
                );
            });

            // Consecutive mode alone should not switch the estimator.
            let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "consecutive mode should keep plain PeakEwma"
            );
            assert!(
                retry_after.is_none(),
                "consecutive mode should not carry a cap"
            );
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn penalize_failures_watch_update() {
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

        let (load_bias, retry_after) = detect_load_bias_and_retry_after(&config);
        assert!(
            load_bias.is_none(),
            "unannotated service should keep plain PeakEwma"
        );
        assert!(
            retry_after.is_none(),
            "unannotated service should have no Retry-After cap"
        );

        parent.meta_mut().annotations = Some(btreemap! {
            "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
        });
        update(&client, parent).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        let (load_bias, _) = detect_load_bias_and_retry_after(&config);
        let lb = load_bias.expect("penalty estimator must be present after update");
        assert_default_penalty_estimator(&lb);
    })
    .await;
}
