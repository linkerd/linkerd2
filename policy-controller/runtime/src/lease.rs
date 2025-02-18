use crate::k8s::{self, api::apps::v1::Deployment, ObjectMeta, Resource};
use anyhow::Result;
use k8s_openapi::api::coordination::v1 as coordv1;
use kube::api::PatchParams;
use std::sync::Arc;
use tokio::{sync::watch, time};

const LEASE_DURATION: time::Duration = time::Duration::from_secs(30);
const LEASE_NAME: &str = "policy-controller-write";
const RENEW_GRACE_PERIOD: time::Duration = time::Duration::from_secs(1);

pub async fn init<T>(
    runtime: &kubert::Runtime<T>,
    ns: &str,
    deployment_name: &str,
    hostname: &str,
) -> Result<watch::Receiver<Arc<kubert::lease::Claim>>> {
    // Fetch the policy-controller deployment so that we can use it as an owner
    // reference of the Lease.
    let api = k8s::Api::<Deployment>::namespaced(runtime.client(), ns);
    let mut tries = 3;
    let deployment = loop {
        tries -= 1;
        let error = match api.get(deployment_name).await {
            Ok(deploy) => {
                tracing::debug!(?deploy, "Found Deployment");
                break deploy;
            }
            Err(k8s::Error::Api(error)) => error.into(),
            Err(k8s::Error::Service(error)) => error,
            Err(k8s::Error::HyperError(error)) => error.into(),
            Err(error) => {
                return Err(error.into());
            }
        };
        if tries == 0 {
            anyhow::bail!(error);
        }
        tracing::warn!(?error, "Failed to fetch deployment, retrying in 1s...");
        time::sleep(time::Duration::from_secs(1)).await;
    };

    let patch = kube::api::Patch::Apply(coordv1::Lease {
        metadata: ObjectMeta {
            name: Some(LEASE_NAME.to_string()),
            namespace: Some(ns.to_string()),
            // Specifying a resource version of "0" means that we will
            // only create the Lease if it does not already exist.
            resource_version: Some("0".to_string()),
            owner_references: Some(vec![deployment.controller_owner_ref(&()).unwrap()]),
            labels: Some(
                [
                    (
                        "linkerd.io/control-plane-component".to_string(),
                        "destination".to_string(),
                    ),
                    ("linkerd.io/control-plane-ns".to_string(), ns.to_string()),
                ]
                .into_iter()
                .collect(),
            ),
            ..Default::default()
        },
        spec: None,
    });
    let patch_params = PatchParams {
        field_manager: Some("policy-controller".to_string()),
        ..Default::default()
    };
    let api = k8s::Api::<coordv1::Lease>::namespaced(runtime.client(), ns);

    // An individual request may timeout or hit a transient error, so we try up to 3 times with a brief pause.
    let mut tries = 3;
    loop {
        tries -= 1;
        let error = match api.patch(LEASE_NAME, &patch_params, &patch).await {
            Ok(lease) => {
                tracing::info!(?lease, "Created Lease");
                break;
            }
            Err(k8s::Error::Api(error)) if error.code >= 500 => error.into(),
            Err(k8s::Error::Api(error)) => {
                tracing::debug!(?error, "Lease already exists");
                break;
            }
            Err(k8s::Error::Service(error)) => error,
            Err(k8s::Error::HyperError(error)) => error.into(),
            Err(error) => {
                return Err(error.into());
            }
        };
        if tries == 0 {
            anyhow::bail!(error);
        }
        tracing::warn!(?error, "Failed to create Lease, retrying in 1s...");
        time::sleep(time::Duration::from_secs(1)).await;
    }

    // Create the lease manager used for trying to claim the policy
    // controller write lease.
    // todo: Do we need to use LeaseManager::field_manager here?
    let params = kubert::lease::ClaimParams {
        lease_duration: LEASE_DURATION,
        renew_grace_period: RENEW_GRACE_PERIOD,
    };
    let (claims, _task) = kubert::lease::LeaseManager::init(api, LEASE_NAME)
        .await?
        .spawn(hostname, params)
        .await?;
    Ok(claims)
}
