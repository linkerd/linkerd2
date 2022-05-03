use crate::k8s::{
    labels,
    policy::{
        AuthorizationPolicy, AuthorizationPolicySpec, MeshTLSAuthentication,
        MeshTLSAuthenticationSpec, NetworkAuthentication, NetworkAuthenticationSpec, Server,
        ServerAuthorization, ServerAuthorizationSpec, ServerSpec,
    },
};
use anyhow::{anyhow, bail, Result};
use futures::future;
use hyper::{body::Buf, http, Body, Request, Response};
use k8s_openapi::api::core::v1::ServiceAccount;
use kube::{core::DynamicObject, Resource, ResourceExt};
use linkerd_policy_controller_k8s_api::{policy::NamespacedTargetRef, Namespace};
use serde::de::DeserializeOwned;
use std::task;
use thiserror::Error;
use tracing::{debug, info, trace, warn};

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

type Review = kube::core::admission::AdmissionReview<DynamicObject>;
type AdmissionRequest = kube::core::admission::AdmissionRequest<DynamicObject>;
type AdmissionResponse = kube::core::admission::AdmissionResponse;
type AdmissionReview = kube::core::admission::AdmissionReview<DynamicObject>;

#[async_trait::async_trait]
trait Validate<T> {
    async fn validate(self, ns: &str, name: &str, spec: T) -> Result<()>;
}

// === impl AdmissionService ===

impl hyper::service::Service<Request<Body>> for Admission {
    type Response = Response<Body>;
    type Error = Error;
    type Future = future::BoxFuture<'static, Result<Response<Body>, Error>>;

    fn poll_ready(&mut self, _cx: &mut task::Context<'_>) -> task::Poll<Result<(), Error>> {
        task::Poll::Ready(Ok(()))
    }

    fn call(&mut self, req: Request<Body>) -> Self::Future {
        trace!(?req);
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
                    warn!(%error, "failed to parse request body");
                    return json_response(AdmissionResponse::invalid(error).into_review());
                }
            };
            trace!(?review);

            let rsp = match review.try_into() {
                Ok(req) => {
                    debug!(?req);
                    admission.admit(req).await
                }
                Err(error) => {
                    warn!(%error, "invalid admission request");
                    AdmissionResponse::invalid(error)
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

    async fn admit(self, req: AdmissionRequest) -> AdmissionResponse {
        if is_kind::<AuthorizationPolicy>(&req) {
            return self.admit_spec::<AuthorizationPolicySpec>(req).await;
        }

        if is_kind::<MeshTLSAuthentication>(&req) {
            return self.admit_spec::<MeshTLSAuthenticationSpec>(req).await;
        }

        if is_kind::<NetworkAuthentication>(&req) {
            return self.admit_spec::<NetworkAuthenticationSpec>(req).await;
        }

        if is_kind::<Server>(&req) {
            return self.admit_spec::<ServerSpec>(req).await;
        };

        if is_kind::<ServerAuthorization>(&req) {
            return self.admit_spec::<ServerAuthorizationSpec>(req).await;
        };

        AdmissionResponse::invalid(format_args!(
            "unsupported resource type: {}.{}.{}",
            req.kind.group, req.kind.version, req.kind.kind
        ))
    }

    async fn admit_spec<T>(self, req: AdmissionRequest) -> AdmissionResponse
    where
        T: DeserializeOwned,
        Self: Validate<T>,
    {
        let rsp = AdmissionResponse::from(&req);

        let kind = req.kind.kind.clone();
        let (ns, name, spec) = match parse_spec::<T>(req) {
            Ok(spec) => spec,
            Err(error) => {
                info!(%error, "Failed to parse {} spec", kind);
                return rsp.deny(error);
            }
        };

        if let Err(error) = self.validate(&ns, &name, spec).await {
            info!(%error, %ns, %name, %kind, "Denied");
            return rsp.deny(error);
        }

        rsp
    }
}

fn is_kind<T>(req: &AdmissionRequest) -> bool
where
    T: Resource,
    T::DynamicType: Default,
{
    let dt = Default::default();
    *req.kind.group == *T::group(&dt) && *req.kind.kind == *T::kind(&dt)
}

fn json_response(rsp: AdmissionReview) -> Result<Response<Body>, Error> {
    let bytes = serde_json::to_vec(&rsp)?;
    Ok(Response::builder()
        .status(http::StatusCode::OK)
        .header(http::header::CONTENT_TYPE, "application/json")
        .body(Body::from(bytes))
        .expect("admission review response must be valid"))
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

#[async_trait::async_trait]
impl Validate<AuthorizationPolicySpec> for Admission {
    async fn validate(self, _ns: &str, _name: &str, spec: AuthorizationPolicySpec) -> Result<()> {
        // TODO support namespace references?
        if !spec.target_ref.targets_kind::<Server>() {
            bail!(
                "invalid targetRef kind: {}",
                spec.target_ref.canonical_kind()
            );
        }

        let mtls_authns_count = spec
            .required_authentication_refs
            .iter()
            .filter(|authn| authn.targets_kind::<MeshTLSAuthentication>())
            .count();
        if mtls_authns_count > 1 {
            bail!("only a single MeshTLSAuthentication may be set");
        }

        let net_authns_count = spec
            .required_authentication_refs
            .iter()
            .filter(|authn| authn.targets_kind::<NetworkAuthentication>())
            .count();
        if net_authns_count > 1 {
            bail!("only a single NetworkAuthentication may be set");
        }

        if mtls_authns_count + net_authns_count < spec.required_authentication_refs.len() {
            let kinds = spec
                .required_authentication_refs
                .iter()
                .filter(|authn| {
                    !authn.targets_kind::<MeshTLSAuthentication>()
                        && !authn.targets_kind::<NetworkAuthentication>()
                })
                .map(|authn| authn.canonical_kind())
                .collect::<Vec<_>>();
            bail!("unsupported authentication kind(s): {}", kinds.join(", "));
        }

        Ok(())
    }
}

fn validate_identity_ref(id: &NamespacedTargetRef) -> Result<()> {
    if id.targets_kind::<ServiceAccount>() {
        return Ok(());
    }

    if id.targets_kind::<Namespace>() {
        if id.namespace.is_some() {
            bail!("Namespace identity_ref is cluster-scoped and cannot have a namespace");
        }
        return Ok(());
    }

    bail!("invalid identity target kind: {}", id.canonical_kind());
}

#[async_trait::async_trait]
impl Validate<MeshTLSAuthenticationSpec> for Admission {
    async fn validate(self, _ns: &str, _name: &str, spec: MeshTLSAuthenticationSpec) -> Result<()> {
        // The CRD validates identity strings, but does not validate identity references.
        for id in spec.identity_refs.iter().flatten() {
            validate_identity_ref(id)?;
        }

        Ok(())
    }
}

#[async_trait::async_trait]
impl Validate<ServerSpec> for Admission {
    /// Checks that `spec` doesn't select the same pod/ports as other existing Servers
    //
    // TODO(ver) this isn't rigorous about detecting servers that select the same port if one port
    // specifies a numeric port and the other specifies the port's name.
    async fn validate(self, ns: &str, name: &str, spec: ServerSpec) -> Result<()> {
        // Since we can't ensure that the local index is up-to-date with the API server (i.e.
        // updates may be delayed), we issue an API request to get the latest state of servers in
        // the namespace.
        let servers = kube::Api::<Server>::namespaced(self.client, ns)
            .list(&kube::api::ListParams::default())
            .await?;
        for server in servers.items.into_iter() {
            if server.name() != name
                && server.spec.port == spec.port
                && Self::overlaps(&server.spec.pod_selector, &spec.pod_selector)
            {
                bail!("identical server spec already exists");
            }
        }

        Ok(())
    }
}

impl Admission {
    /// Detects whether two pod selectors can select the same pod
    //
    // TODO(ver) We can probably detect overlapping selectors more effectively. For
    // example, if `left` selects pods with 'foo=bar' and `right` selects pods with
    // 'foo', we should indicate the selectors overlap. It's a bit tricky to work
    // through all of the cases though, so we'll just punt for now.
    fn overlaps(left: &labels::Selector, right: &labels::Selector) -> bool {
        if left.selects_all() || right.selects_all() {
            return true;
        }

        left == right
    }
}

#[async_trait::async_trait]
impl Validate<NetworkAuthenticationSpec> for Admission {
    async fn validate(self, _ns: &str, _name: &str, spec: NetworkAuthenticationSpec) -> Result<()> {
        if spec.networks.is_empty() {
            bail!("at least one network must be specified");
        }
        for net in spec.networks.into_iter() {
            for except in net.except.into_iter().flatten() {
                if except.contains(&net.cidr) {
                    bail!(
                        "cidr '{}' is completely negated by exception '{}'",
                        net.cidr,
                        except
                    );
                }
                if !net.cidr.contains(&except) {
                    bail!(
                        "cidr '{}' does not include exception '{}'",
                        net.cidr,
                        except
                    );
                }
            }
        }

        Ok(())
    }
}

#[async_trait::async_trait]
impl Validate<ServerAuthorizationSpec> for Admission {
    async fn validate(self, _ns: &str, _name: &str, spec: ServerAuthorizationSpec) -> Result<()> {
        if let Some(mtls) = spec.client.mesh_tls.as_ref() {
            if spec.client.unauthenticated {
                bail!("`unauthenticated` must be false if `mesh_tls` is specified");
            }
            if mtls.unauthenticated_tls {
                let ids = mtls.identities.as_ref().map(|ids| ids.len()).unwrap_or(0);
                let sas = mtls
                    .service_accounts
                    .as_ref()
                    .map(|sas| sas.len())
                    .unwrap_or(0);
                if ids + sas > 0 {
                    bail!("`unauthenticatedTLS` be false if any `identities` or `service_accounts` is specified");
                }
            }
        }

        for net in spec.client.networks.into_iter().flatten() {
            for except in net.except.into_iter().flatten() {
                if except.contains(&net.cidr) {
                    bail!(
                        "cidr '{}' is completely negated by exception '{}'",
                        net.cidr,
                        except
                    );
                }
                if !net.cidr.contains(&except) {
                    bail!(
                        "cidr '{}' does not include exception '{}'",
                        net.cidr,
                        except
                    );
                }
            }
        }

        Ok(())
    }
}
