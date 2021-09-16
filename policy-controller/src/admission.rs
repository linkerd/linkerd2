use crate::api;
use anyhow::{anyhow, bail, Result};
use api::policy::ServerSpec;
use kube::{api::Api, core::DynamicObject, ResourceExt};
use std::convert::{Infallible, TryInto};
use tracing::{debug, info, warn};
use warp::{filters::BoxedFilter, http, reply, Filter};

/// Builds an API handler for a validating admission webhook that examines `Server` resources.
pub fn routes(client: kube::Client) -> BoxedFilter<(impl warp::Reply,)> {
    warp::post()
        .and(warp::path::end())
        // 64KB should be more than enough for any `Server` instance we can expect.
        .and(warp::body::content_length_limit(64 * 1024).and(warp::body::json()))
        .and(warp::any().map(move || client.clone()))
        .and_then(|review: Review, client| async move {
            let req: Request = match review.try_into() {
                Ok(req) => req,
                Err(error) => {
                    warn!(%error, "Invalid admission request");
                    return ok(reply::json(&Response::invalid(error).into_review()));
                }
            };
            debug!(?req);

            let rsp = Response::from(&req);

            // Parse the server instance under review before doing anything with the API--i.e., if
            // this fails we don't have to waste the API calls.
            let (ns, name, review_spec) = match parse_server(req) {
                Ok(s) => s,
                Err(error) => {
                    warn!(%error, "Failed to deserialize server from admission request");
                    return ok(reply::json(&Response::invalid(error).into_review()));
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
                    return ok(http::StatusCode::INTERNAL_SERVER_ERROR);
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
            ok(reply::json(&rsp.into_review()))
        })
        .with(warp::trace::request())
        .boxed()
}

type Review = kube::core::admission::AdmissionReview<DynamicObject>;
type Request = kube::core::admission::AdmissionRequest<DynamicObject>;
type Response = kube::core::admission::AdmissionResponse;

#[inline]
fn ok(reply: impl warp::Reply + 'static) -> Result<Box<dyn warp::Reply>, Infallible> {
    Ok(Box::new(reply))
}

/// Parses a `Server` instance and it's namespace from the admission request.
fn parse_server(req: Request) -> Result<(String, String, api::policy::ServerSpec)> {
    let obj = req.object.ok_or_else(|| anyhow!("missing server"))?;
    let ns = obj
        .namespace()
        .ok_or_else(|| anyhow!("no 'namespace' field set on server"))?;
    let name = obj.name();
    let data = serde_json::from_value::<RequestData>(obj.data)?;
    Ok((ns, name, data.spec))
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

/// RequestData is a wrapper around the `data` field of the AdmissionRequest.
/// The data field contains a `serde_json::value::Value::Object` type that wraps
/// the spec of the resource, deserializing directly to a ServiceSpec won't work
/// in this case.
#[derive(serde::Deserialize, Debug)]
struct RequestData {
    spec: ServerSpec,
}
