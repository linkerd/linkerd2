use super::validation;
use crate::k8s::policy::{
    httproute, server::Selector, AuthorizationPolicy, AuthorizationPolicySpec, HttpRoute,
    HttpRouteSpec, LocalTargetRef, MeshTLSAuthentication, MeshTLSAuthenticationSpec,
    NamespacedTargetRef, NetworkAuthentication, NetworkAuthenticationSpec, Server,
    ServerAuthorization, ServerAuthorizationSpec, ServerSpec, UnmeshedNetwork, UnmeshedNetworkSpec,
};
use anyhow::{anyhow, bail, ensure, Result};
use futures::future;
use hyper::{body::Buf, http, Body, Request, Response};
use k8s_openapi::api::core::v1::{Namespace, ServiceAccount};
use kube::{core::DynamicObject, Resource, ResourceExt};
use linkerd_policy_controller_core as core;
use linkerd_policy_controller_k8s_api::gateway::{self as k8s_gateway_api, GrpcRoute};
use linkerd_policy_controller_k8s_index::{self as index, outbound::index as outbound_index};
use serde::de::DeserializeOwned;
use std::{collections::BTreeMap, task};
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
    async fn validate(
        self,
        ns: &str,
        name: &str,
        annotations: &BTreeMap<String, String>,
        spec: T,
    ) -> Result<()>;
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

        if is_kind::<HttpRoute>(&req) {
            return self.admit_spec::<HttpRouteSpec>(req).await;
        }

        if is_kind::<UnmeshedNetwork>(&req) {
            return self.admit_spec::<UnmeshedNetworkSpec>(req).await;
        }

        if is_kind::<k8s_gateway_api::HttpRoute>(&req) {
            return self.admit_spec::<k8s_gateway_api::HttpRouteSpec>(req).await;
        }

        if is_kind::<k8s_gateway_api::GrpcRoute>(&req) {
            return self.admit_spec::<k8s_gateway_api::GrpcRouteSpec>(req).await;
        }

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
        let (obj, spec) = match parse_spec::<T>(req) {
            Ok(spec) => spec,
            Err(error) => {
                info!(%error, "Failed to parse {} spec", kind);
                return rsp.deny(error);
            }
        };

        let ns = obj.namespace().unwrap_or_default();
        let name = obj.name_any();
        let annotations = obj.annotations();

        if let Err(error) = self.validate(&ns, &name, annotations, spec).await {
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
    req.kind.group.eq_ignore_ascii_case(&T::group(&dt))
        && req.kind.kind.eq_ignore_ascii_case(&T::kind(&dt))
}

fn json_response(rsp: AdmissionReview) -> Result<Response<Body>, Error> {
    let bytes = serde_json::to_vec(&rsp)?;
    Ok(Response::builder()
        .status(http::StatusCode::OK)
        .header(http::header::CONTENT_TYPE, "application/json")
        .body(Body::from(bytes))
        .expect("admission review response must be valid"))
}

fn parse_spec<T: DeserializeOwned>(req: AdmissionRequest) -> Result<(DynamicObject, T)> {
    let obj = req
        .object
        .ok_or_else(|| anyhow!("admission request missing 'object"))?;

    let spec = {
        let data = obj
            .data
            .get("spec")
            .cloned()
            .ok_or_else(|| anyhow!("admission request missing 'spec'"))?;
        serde_json::from_value(data)?
    };

    Ok((obj, spec))
}

/// Validates the target of an `AuthorizationPolicy`.
fn validate_policy_target(ns: &str, tgt: &LocalTargetRef) -> Result<()> {
    if tgt.targets_kind::<Server>() {
        return Ok(());
    }

    if tgt.targets_kind::<HttpRoute>() {
        return Ok(());
    }

    if tgt.targets_kind::<GrpcRoute>() {
        return Ok(());
    }

    if tgt.targets_kind::<Namespace>() {
        if tgt.name != ns {
            bail!("cannot target another namespace: {}", tgt.name);
        }
        return Ok(());
    }

    bail!("invalid targetRef kind: {}", tgt.canonical_kind());
}

#[async_trait::async_trait]
impl Validate<AuthorizationPolicySpec> for Admission {
    async fn validate(
        self,
        ns: &str,
        _name: &str,
        _annotations: &BTreeMap<String, String>,
        spec: AuthorizationPolicySpec,
    ) -> Result<()> {
        validate_policy_target(ns, &spec.target_ref)?;

        let mtls_authns_count = spec
            .required_authentication_refs
            .iter()
            .filter(|authn| authn.targets_kind::<MeshTLSAuthentication>())
            .count();
        if mtls_authns_count > 1 {
            bail!("only a single MeshTLSAuthentication may be set");
        }

        let sa_authns_count = spec
            .required_authentication_refs
            .iter()
            .filter(|authn| authn.targets_kind::<ServiceAccount>())
            .count();
        if sa_authns_count > 1 {
            bail!("only a single ServiceAccount may be set");
        }

        if mtls_authns_count + sa_authns_count > 1 {
            bail!("a MeshTLSAuthentication and ServiceAccount may not be set together");
        }

        let net_authns_count = spec
            .required_authentication_refs
            .iter()
            .filter(|authn| authn.targets_kind::<NetworkAuthentication>())
            .count();
        if net_authns_count > 1 {
            bail!("only a single NetworkAuthentication may be set");
        }

        if mtls_authns_count + sa_authns_count + net_authns_count
            < spec.required_authentication_refs.len()
        {
            let kinds = spec
                .required_authentication_refs
                .iter()
                .filter(|authn| {
                    !authn.targets_kind::<MeshTLSAuthentication>()
                        && !authn.targets_kind::<NetworkAuthentication>()
                        && !authn.targets_kind::<ServiceAccount>()
                })
                .map(|authn| authn.canonical_kind())
                .collect::<Vec<_>>();
            bail!("unsupported authentication kind(s): {}", kinds.join(", "));
        }

        // Confirm that the index will be able to read this spec.
        index::authorization_policy::validate(spec)?;

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
    async fn validate(
        self,
        _ns: &str,
        _name: &str,
        _annotations: &BTreeMap<String, String>,
        spec: MeshTLSAuthenticationSpec,
    ) -> Result<()> {
        for id in spec.identities.iter().flatten() {
            if let Err(err) = validation::validate_identity(id) {
                bail!("id {} is invalid: {}", id, err);
            }
        }

        for id in spec.identity_refs.iter().flatten() {
            validate_identity_ref(id)?;
        }

        Ok(())
    }
}

#[async_trait::async_trait]
impl Validate<ServerSpec> for Admission {
    /// Checks that `spec` doesn't select the same pod/ports as other existing Servers, and that
    /// `accessPolicy` contains a valid value
    //
    // TODO(ver) this isn't rigorous about detecting servers that select the same port if one port
    // specifies a numeric port and the other specifies the port's name.
    async fn validate(
        self,
        ns: &str,
        name: &str,
        _annotations: &BTreeMap<String, String>,
        spec: ServerSpec,
    ) -> Result<()> {
        // Since we can't ensure that the local index is up-to-date with the API server (i.e.
        // updates may be delayed), we issue an API request to get the latest state of servers in
        // the namespace.
        let servers = kube::Api::<Server>::namespaced(self.client, ns)
            .list(&kube::api::ListParams::default())
            .await?;
        for server in servers.items.into_iter() {
            let server_name = server.name_unchecked();
            if server_name != name
                && server.spec.port == spec.port
                && Self::overlaps(&server.spec.selector, &spec.selector)
            {
                let server_ns = server.namespace();
                let server_ns = server_ns.as_deref().unwrap_or("default");
                bail!(
                    "Server spec '{server_ns}/{server_name}' already defines a policy \
                    for port {}, and selects pods that would be selected by this Server",
                    server.spec.port,
                );
            }
        }

        if let Some(policy) = spec.access_policy {
            policy
                .parse::<index::DefaultPolicy>()
                .map_err(|err| anyhow!("Invalid 'accessPolicy' field: {err}"))?;
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
    fn overlaps(left: &Selector, right: &Selector) -> bool {
        match (left, right) {
            (Selector::Pod(ps_left), Selector::Pod(ps_right)) => {
                if ps_left.selects_all() || ps_right.selects_all() {
                    return true;
                }
            }
            (Selector::ExternalWorkload(et_left), Selector::ExternalWorkload(et_right)) => {
                if et_left.selects_all() || et_right.selects_all() {
                    return true;
                }
            }
            (_, _) => return false,
        }

        left == right
    }
}

#[async_trait::async_trait]
impl Validate<NetworkAuthenticationSpec> for Admission {
    async fn validate(
        self,
        _ns: &str,
        _name: &str,
        _annotations: &BTreeMap<String, String>,
        spec: NetworkAuthenticationSpec,
    ) -> Result<()> {
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
impl Validate<UnmeshedNetworkSpec> for Admission {
    async fn validate(
        self,
        _ns: &str,
        _name: &str,
        _annotations: &BTreeMap<String, String>,
        spec: UnmeshedNetworkSpec,
    ) -> Result<()> {
        if spec.networks.is_empty() {
            bail!("at least one network must be specified");
        }

        Ok(())
    }
}

#[async_trait::async_trait]
impl Validate<ServerAuthorizationSpec> for Admission {
    async fn validate(
        self,
        _ns: &str,
        _name: &str,
        _annotations: &BTreeMap<String, String>,
        spec: ServerAuthorizationSpec,
    ) -> Result<()> {
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

use index::routes::http as http_route;

fn validate_match(
    httproute::HttpRouteMatch {
        path,
        headers,
        query_params,
        method,
    }: httproute::HttpRouteMatch,
) -> Result<()> {
    let _ = path.map(index::routes::http::path_match).transpose()?;
    let _ = method
        .as_deref()
        .map(core::routes::Method::try_from)
        .transpose()?;

    for q in query_params.into_iter().flatten() {
        index::routes::http::query_param_match(q)?;
    }

    for h in headers.into_iter().flatten() {
        index::routes::http::header_match(h)?;
    }

    Ok(())
}

#[async_trait::async_trait]
impl Validate<HttpRouteSpec> for Admission {
    async fn validate(
        self,
        _ns: &str,
        _name: &str,
        annotations: &BTreeMap<String, String>,
        spec: HttpRouteSpec,
    ) -> Result<()> {
        if spec
            .inner
            .parent_refs
            .iter()
            .flatten()
            .any(index::outbound::index::is_parent_service)
        {
            index::outbound::index::http::parse_http_retry(annotations)?;
            index::outbound::index::parse_accrual_config(annotations)?;
            index::outbound::index::parse_timeouts(annotations)?;
        }

        fn validate_filter(filter: httproute::HttpRouteFilter) -> Result<()> {
            match filter {
                httproute::HttpRouteFilter::RequestHeaderModifier {
                    request_header_modifier,
                } => index::routes::http::header_modifier(request_header_modifier).map(|_| ()),
                httproute::HttpRouteFilter::ResponseHeaderModifier {
                    response_header_modifier,
                } => index::routes::http::header_modifier(response_header_modifier).map(|_| ()),
                httproute::HttpRouteFilter::RequestRedirect { request_redirect } => {
                    index::routes::http::req_redirect(request_redirect).map(|_| ())
                }
            }
        }

        fn validate_timeouts(timeouts: httproute::HttpRouteTimeouts) -> Result<()> {
            use std::time::Duration;

            if let Some(t) = timeouts.backend_request {
                ensure!(
                    !t.is_negative(),
                    "backendRequest timeout must not be negative"
                );
            }

            if let Some(t) = timeouts.request {
                ensure!(!t.is_negative(), "request timeout must not be negative");
            }

            if let (Some(req), Some(backend_req)) = (timeouts.request, timeouts.backend_request) {
                ensure!(
                    Duration::from(req) >= Duration::from(backend_req),
                    "backendRequest timeout ({backend_req}) must not be greater than request timeout ({req})"
                );
            }
            Ok(())
        }

        // Validate the rules in this spec.
        // This is essentially equivalent to the indexer's conversion function
        // from `HttpRouteSpec` to `InboundRouteBinding`, except that we don't
        // actually allocate stuff in order to return an `InboundRouteBinding`.
        for httproute::HttpRouteRule {
            filters,
            matches,
            timeouts,
            ..
        } in spec.rules.into_iter().flatten()
        {
            for m in matches.into_iter().flatten() {
                validate_match(m)?;
            }

            for f in filters.into_iter().flatten() {
                validate_filter(f)?;
            }

            if let Some(timeouts) = timeouts {
                validate_timeouts(timeouts)?;
            }
        }

        Ok(())
    }
}

#[async_trait::async_trait]
impl Validate<k8s_gateway_api::HttpRouteSpec> for Admission {
    async fn validate(
        self,
        _ns: &str,
        _name: &str,
        annotations: &BTreeMap<String, String>,
        spec: k8s_gateway_api::HttpRouteSpec,
    ) -> Result<()> {
        if spec
            .inner
            .parent_refs
            .iter()
            .flatten()
            .any(outbound_index::is_parent_service)
        {
            outbound_index::http::parse_http_retry(annotations)?;
            outbound_index::parse_accrual_config(annotations)?;
            outbound_index::parse_timeouts(annotations)?;
        }

        fn validate_filter(filter: k8s_gateway_api::HttpRouteFilter) -> Result<()> {
            match filter {
                k8s_gateway_api::HttpRouteFilter::RequestHeaderModifier {
                    request_header_modifier,
                } => index::routes::http::header_modifier(request_header_modifier).map(|_| ()),
                k8s_gateway_api::HttpRouteFilter::ResponseHeaderModifier {
                    response_header_modifier,
                } => index::routes::http::header_modifier(response_header_modifier).map(|_| ()),
                k8s_gateway_api::HttpRouteFilter::RequestRedirect { request_redirect } => {
                    index::routes::http::req_redirect(request_redirect).map(|_| ())
                }
                k8s_gateway_api::HttpRouteFilter::RequestMirror { .. } => Ok(()),
                k8s_gateway_api::HttpRouteFilter::URLRewrite { .. } => Ok(()),
                k8s_gateway_api::HttpRouteFilter::ExtensionRef { .. } => Ok(()),
            }
        }

        // Validate the rules in this spec.
        // This is essentially equivalent to the indexer's conversion function
        // from `HttpRouteSpec` to `InboundRouteBinding`, except that we don't
        // actually allocate stuff in order to return an `InboundRouteBinding`.
        for k8s_gateway_api::HttpRouteRule {
            filters, matches, ..
        } in spec.rules.into_iter().flatten()
        {
            for m in matches.into_iter().flatten() {
                validate_match(m)?;
            }

            for f in filters.into_iter().flatten() {
                validate_filter(f)?;
            }
        }

        Ok(())
    }
}

#[async_trait::async_trait]
impl Validate<k8s_gateway_api::GrpcRouteSpec> for Admission {
    async fn validate(
        self,
        _ns: &str,
        _name: &str,
        annotations: &BTreeMap<String, String>,
        spec: k8s_gateway_api::GrpcRouteSpec,
    ) -> Result<()> {
        if spec
            .inner
            .parent_refs
            .iter()
            .flatten()
            .any(outbound_index::is_parent_service)
        {
            outbound_index::grpc::parse_grpc_retry(annotations)?;
            outbound_index::parse_accrual_config(annotations)?;
            outbound_index::parse_timeouts(annotations)?;
        }

        fn validate_filter(filter: k8s_gateway_api::GrpcRouteFilter) -> Result<()> {
            match filter {
                k8s_gateway_api::GrpcRouteFilter::RequestHeaderModifier {
                    request_header_modifier,
                } => http_route::header_modifier(request_header_modifier).map(|_| ()),
                k8s_gateway_api::GrpcRouteFilter::ResponseHeaderModifier {
                    response_header_modifier,
                } => http_route::header_modifier(response_header_modifier).map(|_| ()),
                k8s_gateway_api::GrpcRouteFilter::RequestMirror { .. } => Ok(()),
                k8s_gateway_api::GrpcRouteFilter::ExtensionRef { .. } => Ok(()),
            }
        }

        fn validate_match_rule(
            k8s_gateway_api::GrpcRouteMatch { method, headers }: k8s_gateway_api::GrpcRouteMatch,
        ) -> Result<()> {
            if let Some(method_match) = method {
                let (method_name, service_name) = match method_match {
                    k8s_gateway_api::GrpcMethodMatch::Exact { method, service } => {
                        (method, service)
                    }
                    k8s_gateway_api::GrpcMethodMatch::RegularExpression { method, service } => {
                        (method, service)
                    }
                };

                if method_name.as_deref().map(str::is_empty).unwrap_or(true)
                    && service_name.as_deref().map(str::is_empty).unwrap_or(true)
                {
                    bail!("at least one of GrpcMethodMatch.Service and GrpcMethodMatch.Method MUST be a non-empty string")
                }
            }

            for rule in headers.into_iter().flatten() {
                http_route::header_match(rule)?;
            }

            Ok(())
        }

        // Validate the rules in this spec.
        // This is essentially just a check to ensure that none
        // of the rules are improperly constructed (e.g. include
        // a `GrpcMethodMatch` rule where neither `method.method`
        // nor `method.service` actually contain a value)
        for k8s_gateway_api::GrpcRouteRule {
            filters, matches, ..
        } in spec.rules.into_iter().flatten()
        {
            for rule in matches.into_iter().flatten() {
                validate_match_rule(rule)?;
            }

            for filter in filters.into_iter().flatten() {
                validate_filter(filter)?;
            }
        }

        Ok(())
    }
}
