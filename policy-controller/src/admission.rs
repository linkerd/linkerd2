use crate::api;
use anyhow::{anyhow, bail, Result};
use api::policy::ServerSpec;
use futures::future;
use hyper::{body::Buf, http, Body, Request, Response};
use kube::{core::DynamicObject, Api, ResourceExt};
use std::task;
use thiserror::Error;
use tracing::{debug, info, warn};

#[derive(Clone)]
pub struct Service {
    pub client: kube::Client,
}

#[derive(Debug, Error)]
pub enum Error {
    #[error("failed to read request body: {0}")]
    Request(
        #[from]
        #[source]
        hyper::Error,
    ),

    #[error("failed to encode json response: {0}")]
    Json(
        #[from]
        #[source]
        serde_json::Error,
    ),
}

impl hyper::service::Service<Request<Body>> for Service {
    type Response = Response<Body>;
    type Error = Error;
    type Future = future::BoxFuture<'static, Result<Response<Body>, Error>>;

    fn poll_ready(&mut self, _cx: &mut task::Context<'_>) -> task::Poll<Result<(), Error>> {
        task::Poll::Ready(Ok(()))
    }

    fn call(&mut self, req: Request<Body>) -> Self::Future {
        if req.method() != http::Method::POST || req.uri().path() != "/" {
            return Box::pin(future::ok(
                Response::builder()
                    .status(http::StatusCode::NOT_FOUND)
                    .body(Body::empty())
                    .expect("not found response must be valid"),
            ));
        }
        let client = self.client.clone();
        Box::pin(async move {
            let bytes = hyper::body::aggregate(req.into_body()).await?;
            let review: Review = match serde_json::from_reader(bytes.reader()) {
                Ok(review) => review,
                Err(error) => {
                    warn!(%error, "Failed to parse request body");
                    return json_response(AdmissionResponse::invalid(error).into_review());
                }
            };

            let req: AdmissionRequest = match review.try_into() {
                Ok(req) => req,
                Err(error) => {
                    warn!(%error, "Invalid admission request");
                    return json_response(AdmissionResponse::invalid(error).into_review());
                }
            };
            debug!(?req);

            let rsp = AdmissionResponse::from(&req);

            // Parse the server instance under review before doing anything with the API--i.e., if
            // this fails we don't have to waste the API calls.
            let (ns, name, review_spec) = match parse_server(req) {
                Ok(s) => s,
                Err(error) => {
                    warn!(%error, "Failed to deserialize server from admission request");
                    return json_response(AdmissionResponse::invalid(error).into_review());
                }
            };

            // Fetch a list of servers so that we can detect conflicts.
            //
            // TODO(ver) We already have a watch on these resources, so we could simply lookup
            // against an index to avoid unnecessary work on the API server.
            let api = Api::<api::policy::Server>::namespaced(client, &*ns);
            let servers = match api.list(&Default::default()).await {
                Ok(servers) => servers,
                Err(error) => {
                    warn!(%error, "Failed to list servers");
                    return Ok(Response::builder()
                        .status(http::StatusCode::INTERNAL_SERVER_ERROR)
                        .body(Body::empty())
                        .expect("error response must be valid"));
                }
            };

            // If validation fails, deny admission.
            let rsp = match validate(&name, &review_spec, &*servers.items) {
                Ok(()) => rsp,
                Err(error) => {
                    info!(%error, %ns, %name, "Denying server");
                    rsp.deny(error)
                }
            };
            debug!(?rsp);
            json_response(rsp.into_review())
        })
    }
}

fn json_response(rsp: AdmissionReview) -> Result<Response<Body>, Error> {
    let bytes = serde_json::to_vec(&rsp)?;
    Ok(Response::builder()
        .status(http::StatusCode::OK)
        .header(http::header::CONTENT_TYPE, "application/json")
        .body(Body::from(bytes))
        .expect("admission review response must be valid"))
}

type Review = kube::core::admission::AdmissionReview<DynamicObject>;
type AdmissionRequest = kube::core::admission::AdmissionRequest<DynamicObject>;
type AdmissionResponse = kube::core::admission::AdmissionResponse;
type AdmissionReview = kube::core::admission::AdmissionReview<DynamicObject>;

/// Parses a `Server` instance and its namespace from the admission request.
fn parse_server(req: AdmissionRequest) -> Result<(String, String, api::policy::ServerSpec)> {
    let obj = req.object.ok_or_else(|| anyhow!("missing server"))?;
    let ns = obj
        .namespace()
        .ok_or_else(|| anyhow!("no 'namespace' field set on server"))?;
    let name = obj.name();
    let data = obj
        .data
        .get("spec")
        .cloned()
        .ok_or_else(|| anyhow!("no 'spec' field set on server"))?;
    let spec = serde_json::from_value::<ServerSpec>(data)?;
    Ok((ns, name, spec))
}

/// Validates a new server (`review`) against existing `servers`.
fn validate(
    review_name: &str,
    review_spec: &api::policy::ServerSpec,
    servers: &[api::policy::Server],
) -> Result<()> {
    for s in servers {
        // If the port and pod selectors select the same resources, fail the admission of the
        // server. Ignore existing instances of this Server (e.g., if the server's metadata is
        // changing).
        if s.name() != review_name
            // TODO(ver) this isn't rigorous about detecting servers that select the same port if one port
            // specifies a numeric port and the other specifies the port's name.
            && s.spec.port == review_spec.port
            // TODO(ver) We can probably detect overlapping selectors more effectively.
            && s.spec.pod_selector == review_spec.pod_selector
        {
            bail!("identical server spec already exists");
        }
    }

    Ok(())
}
