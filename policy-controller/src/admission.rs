use kube::core::{
    admission::{AdmissionRequest, AdmissionResponse, AdmissionReview},
    DynamicObject,
};
use std::convert::{Infallible, TryInto};
use warp::{reply, Reply};
use tracing::{error, info, instrument};

// A general /mutate handler, handling errors from the underlying business logic
#[instrument]
pub async fn mutate_handler(body: AdmissionReview<DynamicObject>) -> Result<impl Reply, Infallible> {
    // Parse incoming webhook AdmissionRequest first
    let req: AdmissionRequest<_> = match body.try_into() {
        Ok(req) => req,
        Err(err) => {
            error!(error = ?err.to_string(), "invalid request");
            return Ok(reply::json(
                &AdmissionResponse::invalid(err.to_string()).into_review(),
            ));
        }
    };
    info!(?req);

    // Then construct a AdmissionResponse
    let res = AdmissionResponse::from(&req);
    // Wrap the AdmissionResponse wrapped in an AdmissionReview
    Ok(reply::json(&res.into_review()))
}