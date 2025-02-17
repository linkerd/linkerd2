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
    let deployment = api.get(deployment_name).await?;

    let lease = coordv1::Lease {
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
    };
    let api = k8s::Api::<coordv1::Lease>::namespaced(runtime.client(), ns);
    match api
        .patch(
            LEASE_NAME,
            &PatchParams {
                field_manager: Some("policy-controller".to_string()),
                ..Default::default()
            },
            &kube::api::Patch::Apply(lease),
        )
        .await
    {
        Ok(lease) => tracing::info!(?lease, "Created Lease resource"),
        Err(k8s::Error::Api(_)) => tracing::debug!("Lease already exists, no need to create it"),
        Err(error) => {
            return Err(error.into());
        }
    };

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
