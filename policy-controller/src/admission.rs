use crate::api;
use anyhow::{anyhow, bail, Result};
use api::policy::ServerSpec;
use futures::future;
use hyper::{body::Buf, http, Body, Request, Response};
use kube::{core::DynamicObject, ResourceExt};
use linkerd_policy_controller_k8s_index::{Index, SharedIndex};
use std::task;
use thiserror::Error;
use tracing::{debug, info, warn};

#[derive(Clone)]
pub struct Service {
    pub index: SharedIndex,
}

#[derive(Debug, Error)]
pub enum Error {
    #[error("failed to read request body: {0}")]
    Request(#[from] hyper::Error),

    #[error("failed to encode json response: {0}")]
    Json(#[from] serde_json::Error),
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
<<<<<<< HEAD
        let this = self.clone();
||||||| d4543cd8
        let client = self.client.clone();
=======

        let index = self.index.clone();
>>>>>>> ver/policy-admission-cached
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

<<<<<<< HEAD
            let servers = match list_servers(client.clone(), &*ns).await {
                Ok(servers) => servers,
                Err(error) => {
                    warn!(%error, "Failed to list servers");
                    return Ok(Response::builder()
                        .status(http::StatusCode::INTERNAL_SERVER_ERROR)
                        .body(Body::empty())
                        .expect("error response must be valid"));
                }
            };

||||||| d4543cd8
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

=======
>>>>>>> ver/policy-admission-cached
            // If validation fails, deny admission.
<<<<<<< HEAD
            let rsp = match validate(&name, &review_spec, &*servers) {
||||||| d4543cd8
            let rsp = match validate(&name, &review_spec, &*servers.items) {
=======
            let rsp = match validate(&ns, &name, &review_spec, &*index.read()) {
>>>>>>> ver/policy-admission-cached
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

/// Fetch a list of all servers in a namespace.
///
/// TODO(ver) We already have a watch on these resources, so we could simply lookup
/// against an index to avoid unnecessary work on the API server.
async fn list_servers(client: kube::Client, ns: &str) -> kube::Result<Vec<api::policy::Server>> {
    let api = Api::namespaced(client, ns);
    let list = api.list(&kube::api::ListParams::default()).await?;
    Ok(list.items)
}

/// Validates a new server (`review`) against existing `servers`.
fn validate(ns: &str, name: &str, spec: &api::policy::ServerSpec, index: &Index) -> Result<()> {
    if let Some(nsidx) = index.get_ns(ns) {
        for (srvname, srv) in nsidx.servers.iter() {
            // If the port and pod selectors select the same resources, fail the admission of the
            // server. Ignore existing instances of this Server (e.g., if the server's metadata is
            // changing).
            if srvname != name
                // TODO(ver) this isn't rigorous about detecting servers that select the same port if one port
                // specifies a numeric port and the other specifies the port's name.
                && *srv.port() == spec.port
                // TODO(ver) We can probably detect overlapping selectors more effectively.
                && *srv.pod_selector() == spec.pod_selector
            {
                bail!("identical server spec already exists");
            }
        }
    }

    Ok(())
}
