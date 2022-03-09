use crate::api;
use anyhow::{anyhow, bail, Result};
use api::policy::ServerSpec;
use futures::future;
use hyper::{body::Buf, http, Body, Request, Response};
use kube::{core::DynamicObject, ResourceExt};
use std::task;
use thiserror::Error;
use tracing::{debug, info, warn};

#[derive(Clone)]
pub struct Admission {
    client: kube::Client,
}

#[derive(Debug, Error)]
pub enum Error {
    #[error("failed to read request body: {0}")]
    Request(#[from] hyper::Error),

    #[error("failed to encode json response: {0}")]
    Json(#[from] serde_json::Error),
}

impl hyper::service::Service<Request<Body>> for Admission {
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

        let admission = self.clone();
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

            // If validation fails, deny admission.
            let rsp = match admission.validate(&ns, &name, review_spec).await {
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

impl Admission {
    pub fn new(client: kube::Client) -> Self {
        Self { client }
    }

    /// Checks that `spec` doesn't select the same pod/ports as other existing Servers
    //
    // TODO(ver) this isn't rigorous about detecting servers that select the same port if one port
    // specifies a numeric port and the other specifies the port's name.
    async fn validate(self, ns: &str, name: &str, spec: api::policy::ServerSpec) -> Result<()> {
        // Since we can't ensure that the local index is up-to-date with the API server (i.e.
        // updates may be delayed), we issue an API request to get the latest state of servers in
        // the namespace.
        let servers = kube::Api::<api::policy::Server>::namespaced(self.client, ns)
            .list(&kube::api::ListParams::default())
            .await?;
        for server in servers.items.into_iter() {
            if server.name() != name
                && server.spec.port == spec.port
                && overlaps(&server.spec.pod_selector, &spec.pod_selector)
            {
                bail!("identical server spec already exists");
            }
        }

        Ok(())
    }
}

/// Detects whether two pod selectors can select the same pod
//
// TODO(ver) We can probably detect overlapping selectors more effectively. For example, if `left`
// selects pods with 'foo=bar' and `right` selects pods with 'foo', we should indicate the selectors
// overlap. It's a bit tricky to work through all of the cases though, so we'll just punt for now.
fn overlaps(left: &api::labels::Selector, right: &api::labels::Selector) -> bool {
    if left.selects_all() || right.selects_all() {
        return true;
    }

    left == right
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
