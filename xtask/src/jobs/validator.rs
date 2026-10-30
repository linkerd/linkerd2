//! Readiness checks for the policy validator admission webhook.
//!
//! `just policy-validator-ready` runs these after `linkerd-install` and before
//! the policy tests in `policy-test/`, which create invalid resources and
//! expect the webhook to deny them. The webhook is registered with
//! `failurePolicy: Ignore`, so until it is reachable the apiserver admits those
//! resources and the tests fail for reasons unrelated to the code under test.
//! We first wait for a ready endpoint, then until a denial is observed.

use crate::error::XtaskError;
use futures::{future, StreamExt};
use k8s_openapi::api::discovery::v1::EndpointSlice;
use kube::{
    api::{ApiResource, DynamicObject, GroupVersionKind, PostParams},
    runtime::{watcher, WatchStreamExt},
    Api,
};
use std::{pin::pin, time::Duration};
use tokio::time::{self, MissedTickBehavior};
use tokio_stream::wrappers::IntervalStream;

/// Namespace of the Linkerd control plane.
const NAMESPACE: &str = "linkerd";
/// Service fronting the policy validator webhook.
const SERVICE: &str = "linkerd-policy-validator";
/// Webhook name as registered in `charts/linkerd-control-plane/templates/destination-rbac.yaml`.
const WEBHOOK: &str = "linkerd-policy-validator.linkerd.io";
/// Upper bound for each of the endpoint wait and the denial probe.
const TIMEOUT: Duration = Duration::from_secs(60);
/// Delay between attempts.
const INTERVAL: Duration = Duration::from_secs(1);
/// Namespace the probe policy is dry-run created in; nothing is persisted.
const PROBE_NAMESPACE: &str = "default";
/// Name of the probe policy.
const PROBE_NAME: &str = "webhook-probe";

/// Waits for the policy validator Service to have a ready endpoint.
pub struct PolicyValidatorWaitEndpoint {
    /// Client for the cluster running the Linkerd control plane.
    client: kube::Client,
}

impl PolicyValidatorWaitEndpoint {
    /// Creates a checker that talks to the cluster through `client`.
    pub fn new(client: kube::Client) -> Self {
        Self { client }
    }

    /// Waits until the validator Service has a ready endpoint.
    ///
    /// Watches `discovery.k8s.io/v1` EndpointSlices, since core v1 Endpoints is
    /// deprecated, and checks each applied slice as it arrives.
    pub async fn run(&self) -> Result<(), XtaskError> {
        let api = Api::<EndpointSlice>::namespaced(self.client.clone(), NAMESPACE);
        let config =
            watcher::Config::default().labels(&format!("kubernetes.io/service-name={SERVICE}"));

        // One slice with a ready endpoint is enough for the webhook to be called,
        // so no store of all slices is kept. Watch errors are retried by the
        // backoff stream, so they are skipped.
        let ready = watcher(api, config)
            .default_backoff()
            .applied_objects()
            .filter(|slice| future::ready(slice.as_ref().is_ok_and(Self::has_ready_endpoint)));
        let mut ready = pin!(ready);

        time::timeout(TIMEOUT, ready.next()).await.map_err(|_| {
            // No ready endpoint means the webhook cannot be called, so the policy
            // tests would see their invalid resources admitted; fail before they run.
            XtaskError::PolicyValidator(format!(
                "no ready endpoints for {NAMESPACE}/{SERVICE} after {}s",
                TIMEOUT.as_secs(),
            ))
        })?;

        Ok(())
    }

    /// Checks whether `slice` holds an endpoint with addresses that is ready.
    ///
    /// An endpoint counts as ready unless its `ready` condition is explicitly
    /// false, since the API defines an unset condition as ready.
    fn has_ready_endpoint(slice: &EndpointSlice) -> bool {
        slice.endpoints.iter().any(|endpoint| {
            !endpoint.addresses.is_empty()
                && endpoint.conditions.as_ref().and_then(|c| c.ready) != Some(false)
        })
    }
}

/// Waits for the policy validator webhook to deny an invalid policy.
pub struct PolicyValidatorDenial {
    /// Client for the cluster running the Linkerd control plane.
    client: kube::Client,
}

impl PolicyValidatorDenial {
    /// Creates a checker that talks to the cluster through `client`.
    pub fn new(client: kube::Client) -> Self {
        Self { client }
    }

    /// Waits until a server-side dry-run of an invalid AuthorizationPolicy is denied.
    ///
    /// An endpoint alone does not prove admission works: only the denial shows
    /// the webhook is reachable through the proxy with a valid CA bundle. No
    /// watchable signal exists for that, so the probe is retried on an interval.
    pub async fn run(&self) -> Result<(), XtaskError> {
        let resource = ApiResource::from_gvk_with_plural(
            &GroupVersionKind::gvk("policy.linkerd.io", "v1alpha1", "AuthorizationPolicy"),
            "authorizationpolicies",
        );
        let api =
            Api::<DynamicObject>::namespaced_with(self.client.clone(), PROBE_NAMESPACE, &resource);
        let policy = Self::invalid_policy(&resource);
        let params = PostParams {
            dry_run: true,
            ..Default::default()
        };
        let mut ticks = time::interval(INTERVAL);
        ticks.set_missed_tick_behavior(MissedTickBehavior::Delay);

        // Admission and transient failures are retried alike, so only the denial
        // passes the filter. The timeout also bounds a create that never returns.
        let denied = IntervalStream::new(ticks)
            .then(|_| api.create(&params, &policy))
            .map(Self::classify_probe)
            .filter(|probe| future::ready(probe.is_ok()));
        let mut denied = pin!(denied);

        time::timeout(TIMEOUT, denied.next()).await.map_err(|_| {
            // No denial means the policy tests would see their invalid resources
            // admitted; fail before they run.
            XtaskError::PolicyValidator(format!(
                "webhook did not deny probe policy after {}s",
                TIMEOUT.as_secs(),
            ))
        })?;

        Ok(())
    }

    /// Builds an AuthorizationPolicy the validator always rejects.
    ///
    /// The validator allows a single MeshTLSAuthentication reference, so two
    /// make the policy invalid regardless of what else exists in the cluster.
    fn invalid_policy(resource: &ApiResource) -> DynamicObject {
        DynamicObject::new(PROBE_NAME, resource)
            .within(PROBE_NAMESPACE)
            .data(serde_json::json!({
                "spec": {
                    "targetRef": {
                        "group": "policy.linkerd.io",
                        "kind": "Server",
                        "name": "probe",
                    },
                    "requiredAuthenticationRefs": [
                        {"group": "policy.linkerd.io", "kind": "MeshTLSAuthentication", "name": "a"},
                        {"group": "policy.linkerd.io", "kind": "MeshTLSAuthentication", "name": "b"},
                    ],
                },
            }))
    }

    /// Checks that the apiserver response to the probe create is the webhook denial.
    ///
    /// The error describes any other response, for the timeout report.
    fn classify_probe(response: Result<DynamicObject, kube::Error>) -> Result<(), XtaskError> {
        // The apiserver prefixes webhook denials with this text and raises any
        // code below 400 to 400, so only `>= 400` is guaranteed.
        // https://github.com/kubernetes/apiserver/blob/a1b8e6fa9269c795a50d34e22fef9b0bc55f9fdb/pkg/admission/plugin/webhook/errors/statuserror.go#L29
        let denial = format!("admission webhook \"{WEBHOOK}\" denied the request");
        let last = match response {
            // Success means the webhook call failed and failurePolicy=Ignore admitted
            // the probe, as it would the policy tests' invalid resources.
            Ok(_) => "dry-run create was admitted".to_string(),
            Err(kube::Error::Api(status))
                if status.code >= 400 && status.message.contains(&denial) =>
            {
                return Ok(());
            }
            Err(kube::Error::Api(status)) => format!("HTTP {}: {}", status.code, status.message),
            Err(error) => format!("request failed: {error}"),
        };
        Err(XtaskError::PolicyValidator(last))
    }
}
