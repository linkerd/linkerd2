use super::api::policy::{Server, ServerSpec};
use kube::api::{Api, ListParams};
use kube::core::{
    admission::{AdmissionRequest, AdmissionResponse, AdmissionReview},
    DynamicObject, ObjectList,
};
use std::convert::{Infallible, TryInto};
use std::fmt;
use tracing::{error, info, instrument};
use warp::{reply, Reply};

#[derive(Clone)]
pub struct Admission(pub kube::Client);

// A general /mutate handler, handling errors from the underlying business logic
#[instrument]
pub async fn mutate_handler(
    body: AdmissionReview<DynamicObject>,
    admission: Admission,
) -> Result<impl Reply, Infallible> {
    // Parse incoming webhook AdmissionRequest first
    let req: AdmissionRequest<DynamicObject> = match body.try_into() {
        Ok(req) => req,
        Err(err) => {
            error!(error = ?err.to_string(), "invalid request");
            return Ok(reply::json(
                &AdmissionResponse::invalid(err.to_string()).into_review(),
            ));
        }
    };
    info!(?req);

    let api: Api<Server> = Api::all(admission.0.clone());
    let params = ListParams::default();
    let servers = api.list(&params).await.unwrap_or_else(|err| {
        error!(error = ?err.to_string(), "failed to list servers");
        ObjectList {
            metadata: Default::default(),
            items: Default::default(),
        }
    });

    // Then construct a AdmissionResponse
    let mut res = AdmissionResponse::from(&req);

    let conflict = req
        .object
        .and_then(|obj| obj.data.clone().get_mut("spec").cloned())
        .and_then(|spec| {
            serde_json::from_value::<ServerSpec>(spec)
                .map_err(|err| {
                    error!(error = ?err.to_string(), "failed to deserialize");
                    err
                })
                .ok()
        })
        .and_then(|spec| {
            for s in servers {
                if s.spec == spec {
                    return Some("identical server spec already exists");
                }
            }
            None
        });

    // Wrap the AdmissionResponse wrapped in an AdmissionReview
    match conflict {
        Some(err) => {
            res = res.deny(err);
            Ok(reply::json(&res.into_review()))
        }
        None => Ok(reply::json(&res.into_review())),
    }
}

impl fmt::Debug for Admission {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Admission").finish()
    }
}
