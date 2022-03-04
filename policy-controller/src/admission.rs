use crate::api::policy::{
    AuthorizationPolicy, AuthorizationPolicySpec, MeshTLSAuthentication, MeshTLSAuthenticationSpec,
    NetworkAuthentication, NetworkAuthenticationSpec, Server, ServerSpec,
};
use anyhow::{anyhow, bail, Result};
use futures::future;
use hyper::{body::Buf, http, Body, Request, Response};
use k8s_openapi::api::core::v1::ServiceAccount;
use kube::{core::DynamicObject, Resource, ResourceExt};
use linkerd_policy_controller_k8s_index::SharedIndex;
use serde::de::DeserializeOwned;
use std::{net::IpAddr, task};
use thiserror::Error;
use tracing::{debug, info, warn};

#[derive(Clone)]
pub struct AdmissionService {
    pub index: SharedIndex,
}

#[derive(Debug, Error)]
pub enum Error {
    #[error("failed to read request body: {0}")]
    Request(#[from] hyper::Error),

    #[error("failed to encode json response: {0}")]
    Json(#[from] serde_json::Error),
}

type Review = kube::core::admission::AdmissionReview<DynamicObject>;
type AdmissionRequest = kube::core::admission::AdmissionRequest<DynamicObject>;
type AdmissionResponse = kube::core::admission::AdmissionResponse;
type AdmissionReview = kube::core::admission::AdmissionReview<DynamicObject>;

trait Validate {
    fn validate(&self, ns: &str, name: &str, index: &SharedIndex) -> Result<()>;
}

// === impl AdmissionService ===

impl hyper::service::Service<Request<Body>> for AdmissionService {
    type Response = Response<Body>;
    type Error = Error;
    type Future = future::BoxFuture<'static, Result<Response<Body>, Error>>;

    fn poll_ready(&mut self, _cx: &mut task::Context<'_>) -> task::Poll<Result<(), Error>> {
        task::Poll::Ready(Ok(()))
    }

    fn call(&mut self, req: Request<Body>) -> Self::Future {
        tracing::trace!(?req);
        if req.method() != http::Method::POST || req.uri().path() != "/" {
            return Box::pin(future::ok(
                Response::builder()
                    .status(http::StatusCode::NOT_FOUND)
                    .body(Body::empty())
                    .expect("not found response must be valid"),
            ));
        }

        let index = self.index.clone();
        Box::pin(async move {
            let bytes = hyper::body::aggregate(req.into_body()).await?;
            let review: Review = match serde_json::from_reader(bytes.reader()) {
                Ok(review) => review,
                Err(error) => {
                    warn!(%error, "failed to parse request body");
                    return json_response(AdmissionResponse::invalid(error).into_review());
                }
            };
            tracing::trace!(?review);

            let rsp = review
                .try_into()
                .map_err(anyhow::Error::from)
                .and_then(|req| {
                    debug!(?req);
                    admit(req, &index)
                })
                .unwrap_or_else(|error| {
                    warn!(%error, "invalid admission request");
                    AdmissionResponse::invalid(error)
                });

            // If validation fails, deny admission.
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

fn admit(req: AdmissionRequest, index: &SharedIndex) -> Result<AdmissionResponse> {
    if is_kind::<AuthorizationPolicy>(&req) {
        return admit_spec::<AuthorizationPolicySpec>(req, index);
    }

    if is_kind::<MeshTLSAuthentication>(&req) {
        return admit_spec::<MeshTLSAuthenticationSpec>(req, index);
    }

    if is_kind::<NetworkAuthentication>(&req) {
        return admit_spec::<NetworkAuthenticationSpec>(req, index);
    }

    if is_kind::<Server>(&req) {
        return admit_spec::<ServerSpec>(req, index);
    };

    bail!(
        "unsupported resource type: {}.{}.{}",
        req.kind.group,
        req.kind.version,
        req.kind.kind
    )
}

fn is_kind<T>(req: &AdmissionRequest) -> bool
where
    T: Resource,
    T::DynamicType: Default,
{
    let dt = Default::default();
    *req.kind.group == *T::group(&dt) && *req.kind.kind == *T::kind(&dt)
}

fn admit_spec<T: DeserializeOwned + Validate>(
    req: AdmissionRequest,
    index: &SharedIndex,
) -> Result<AdmissionResponse> {
    let kind = req.kind.kind.clone();
    let rsp = AdmissionResponse::from(&req);
    let (ns, name, spec) =
        parse_spec::<T>(req).map_err(|e| e.context(format!("failed to deserialize {}", kind)))?;
    match spec.validate(&ns, &name, index) {
        Ok(()) => Ok(rsp),
        Err(error) => {
            info!(%error, %ns, %name, %kind, "denied");
            Ok(rsp.deny(error))
        }
    }
}

fn parse_spec<T: DeserializeOwned>(req: AdmissionRequest) -> Result<(String, String, T)> {
    let obj = req
        .object
        .ok_or_else(|| anyhow!("admission request missing 'object"))?;

    let ns = obj
        .namespace()
        .ok_or_else(|| anyhow!("admission request missing 'namespace'"))?;
    let name = obj.name();

    let spec = {
        let data = obj
            .data
            .get("spec")
            .cloned()
            .ok_or_else(|| anyhow!("admission request missing 'spec'"))?;
        serde_json::from_value(data)?
    };

    Ok((ns, name, spec))
}

impl Validate for AuthorizationPolicySpec {
    fn validate(&self, _ns: &str, _name: &str, _idx: &SharedIndex) -> Result<()> {
        // TODO support namespace references?
        if !self.target_ref.targets_kind::<Server>() {
            bail!("invalid targetRef kind");
        }
        assert!(
            self.target_ref.namespace.is_none(),
            "authorization policy targetRef namespace cannot be set (in the CRD)"
        );

        if self.required_authentication_refs.is_empty() {
            bail!("at least one authentication reference is required");
        }
        for authn in self.required_authentication_refs.iter() {
            if !authn.targets_kind::<MeshTLSAuthentication>()
                && !authn.targets_kind::<NetworkAuthentication>()
            {
                bail!("unsupported authentication kind");
            }
        }

        Ok(())
    }
}

impl Validate for MeshTLSAuthenticationSpec {
    fn validate(&self, _ns: &str, _name: &str, _idx: &SharedIndex) -> Result<()> {
        // The CRD validates identity strings, but does not validate identity references.

        for id in self.identity_refs.iter().flatten() {
            // TODO support namespace references?
            if !id.targets_kind::<ServiceAccount>() {
                bail!(
                    "invalid identity target kind {}/{}",
                    id.group.as_deref().unwrap_or("core"),
                    id.kind
                );
            }
        }

        Ok(())
    }
}

impl Validate for NetworkAuthenticationSpec {
    fn validate(&self, _ns: &str, _name: &str, _idx: &SharedIndex) -> Result<()> {
        use std::str::FromStr;

        for net in self.networks.iter() {
            let cidr = ipnet::IpNet::from_str(&*net.cidr)
                .map_err(|e| anyhow!(e).context("invalid 'cidr'"))?;

            for except in net.except.iter().flatten() {
                let except = match ipnet::IpNet::from_str(&*except) {
                    Ok(net) => net,
                    Err(error) => match IpAddr::from_str(&*except) {
                        Ok(addr) => addr.into(),
                        Err(_) => bail!("invalid 'except' network: {}", error),
                    },
                };
                if except.contains(&cidr) {
                    bail!(
                        "cidr '{}' is completely negated by exception '{}'",
                        cidr,
                        except
                    );
                }
                if !cidr.contains(&except) {
                    bail!("cidr '{}' does not include exception '{}'", cidr, except);
                }
            }
        }

        Ok(())
    }
}

impl Validate for ServerSpec {
    /// Validates a new server (`review`) against existing `servers`.
    fn validate(&self, ns: &str, name: &str, index: &SharedIndex) -> Result<()> {
        if let Some(nsidx) = index.read().get_ns(ns) {
            for (srvname, srv) in nsidx.servers.iter() {
                // If the port and pod selectors select the same resources, fail the admission of
                // the server. Ignore existing instances of this Server (e.g., if the server's
                // metadata is changing).
                if *srvname != name
                    // TODO(ver) this isn't rigorous about detecting servers that select the same
                    // port if one port specifies a numeric port and the other specifies the port's
                    // name.
                    && *srv.port() == self.port
                    // TODO(ver) We can probably detect overlapping selectors more effectively.
                    && *srv.pod_selector() == self.pod_selector
                {
                    bail!("identical server spec already exists");
                }
            }
        }

        Ok(())
    }
}
