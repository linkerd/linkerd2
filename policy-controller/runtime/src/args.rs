use crate::{
    admission::Admission,
    core::IpNet,
    grpc::{self, metrics::GrpcServerMetricsFamily},
    index::{self, ports::parse_portset, ClusterInfo, DefaultPolicy},
    index_list::IndexList,
    k8s::{self, gateway, Client, Resource},
    lease, status, InboundDiscover, OutboundDiscover,
};
use anyhow::{bail, Result};
use clap::Parser;
use futures::prelude::*;
use kube::{core::DeserializeGuard, runtime::watcher, ResourceExt};
use prometheus_client::registry::Registry;
use serde::de::DeserializeOwned;
use std::fmt::Debug;
use std::{net::SocketAddr, sync::Arc};
use tokio::{sync::mpsc, time::Duration};
use tonic::transport::Server;
use tracing::{info, info_span, instrument, Instrument};

const DETECT_TIMEOUT: Duration = Duration::from_secs(10);
const RECONCILIATION_PERIOD: Duration = Duration::from_secs(10);

// The maximum number of status patches to buffer. As a conservative estimate,
// we assume that sending a patch will take at least 1ms, so we set the buffer
// size to be the same as the reconciliation period in milliseconds.
const STATUS_UPDATE_QUEUE_SIZE: usize = RECONCILIATION_PERIOD.as_millis() as usize;

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "A policy resource controller")]
pub struct Args {
    #[clap(
        long,
        default_value = "linkerd=info,warn",
        env = "LINKERD_POLICY_CONTROLLER_LOG"
    )]
    log_level: kubert::LogFilter,

    #[clap(long, default_value = "plain")]
    log_format: kubert::LogFormat,

    #[clap(flatten)]
    client: kubert::ClientArgs,

    #[clap(flatten)]
    server: kubert::ServerArgs,

    #[clap(flatten)]
    admin: kubert::AdminArgs,

    /// Disables the admission controller server.
    #[clap(long)]
    admission_controller_disabled: bool,

    #[clap(long, default_value = "0.0.0.0:8090")]
    grpc_addr: SocketAddr,

    /// Network CIDRs of pod IPs.
    ///
    /// The default includes all private networks.
    #[clap(
        long,
        default_value = "10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16"
    )]
    cluster_networks: IpNets,

    #[clap(long, default_value = "cluster.local")]
    identity_domain: String,

    #[clap(long, default_value = "cluster.local")]
    cluster_domain: String,

    #[clap(long, default_value = "all-unauthenticated")]
    default_policy: DefaultPolicy,

    #[clap(long, default_value = "linkerd-destination")]
    policy_deployment_name: String,

    #[clap(long, default_value = "linkerd")]
    control_plane_namespace: String,

    /// Network CIDRs of all expected probes.
    #[clap(long)]
    probe_networks: Option<IpNets>,

    #[clap(long)]
    default_opaque_ports: String,

    #[clap(long, default_value = "5000")]
    patch_timeout_ms: u64,

    #[clap(long)]
    allow_l5d_request_headers: bool,

    #[clap(long, default_value = "linkerd-egress")]
    global_egress_network_namespace: String,
}

impl Args {
    #[inline]
    pub async fn parse_and_run() -> Result<()> {
        Self::parse().run().await
    }

    pub async fn run(self) -> Result<()> {
        let Self {
            admin,
            client,
            log_level,
            log_format,
            server,
            grpc_addr,
            admission_controller_disabled,
            identity_domain,
            cluster_domain,
            cluster_networks: IpNets(cluster_networks),
            default_policy,
            policy_deployment_name,
            control_plane_namespace,
            probe_networks,
            default_opaque_ports,
            patch_timeout_ms,
            allow_l5d_request_headers,
            global_egress_network_namespace,
        } = self;

        let server = if admission_controller_disabled {
            None
        } else {
            Some(server)
        };

        let probe_networks = probe_networks.map(|IpNets(nets)| nets).unwrap_or_default();
        let global_egress_network_namespace = Arc::new(global_egress_network_namespace);
        let default_opaque_ports = parse_portset(&default_opaque_ports)?;
        let cluster_info = Arc::new(ClusterInfo {
            networks: cluster_networks.clone(),
            identity_domain,
            control_plane_ns: control_plane_namespace.clone(),
            dns_domain: cluster_domain.clone(),
            default_policy,
            default_detect_timeout: DETECT_TIMEOUT,
            default_opaque_ports,
            probe_networks,
            global_egress_network_namespace,
        });

        // Build the API index data structures which will maintain information
        // necessary for serving the inbound policy and outbound policy gRPC APIs.
        let inbound_index = index::inbound::Index::shared(cluster_info.clone());
        let outbound_index = index::outbound::Index::shared(cluster_info.clone());

        let mut prom = <Registry>::default();
        let resource_status = prom.sub_registry_with_prefix("resource_status");
        let status_metrics = status::ControllerMetrics::register(resource_status);
        let status_index_metrcs = status::IndexMetrics::register(resource_status);

        index::outbound::metrics::register(
            prom.sub_registry_with_prefix("outbound_index"),
            outbound_index.clone(),
        );
        index::inbound::metrics::register(
            prom.sub_registry_with_prefix("inbound_index"),
            inbound_index.clone(),
        );
        let rt_metrics = kubert::RuntimeMetrics::register(prom.sub_registry_with_prefix("kube"));
        let grpc_metrics = grpc::metrics::GrpcServerMetricsFamily::register(
            prom.sub_registry_with_prefix("grpc_server"),
        );

        let mut runtime = kubert::Runtime::builder()
            .with_log(log_level, log_format)
            .with_metrics(rt_metrics)
            .with_admin(admin.into_builder().with_prometheus(prom))
            .with_client(client)
            .with_optional_server(server)
            .build()
            .await?;

        let hostname =
            std::env::var("HOSTNAME").expect("Failed to fetch `HOSTNAME` environment variable");

        let (claims, lease_task) = lease::init(
            &runtime,
            &control_plane_namespace,
            &policy_deployment_name,
            &hostname,
        )
        .await?;

        // Determine which version of the TLSRoute API the cluster serves, if
        // any. This must happen before the status index and controller are
        // built, since they address routes by version.
        let tls_route_api_version = tls_route_api_version(&runtime.client()).await;

        // Build the status index which will maintain information necessary for
        // updating the status field of policy resources.
        let (updates_tx, updates_rx) = mpsc::channel(STATUS_UPDATE_QUEUE_SIZE);
        let status_index = status::Index::shared(
            hostname.clone(),
            claims.clone(),
            updates_tx,
            status_index_metrcs,
            cluster_networks.clone(),
            tls_route_api_version.unwrap_or_default(),
        );

        // Spawn resource watches.

        let pods = guarded_watch::<k8s::Pod, _>(
            &mut runtime,
            watcher::Config::default().labels("linkerd.io/control-plane-ns"),
        );

        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), pods).instrument(info_span!("pods")),
        );

        let external_workloads = guarded_watch::<k8s::external_workload::ExternalWorkload, _>(
            &mut runtime,
            watcher::Config::default(),
        );
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), external_workloads)
                .instrument(info_span!("external_workloads")),
        );

        let servers =
            guarded_watch::<k8s::policy::Server, _>(&mut runtime, watcher::Config::default());
        let servers_indexes = IndexList::new(inbound_index.clone())
            .push(status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(servers_indexes, servers).instrument(info_span!("servers")),
        );

        let server_authzs = guarded_watch::<k8s::policy::ServerAuthorization, _>(
            &mut runtime,
            watcher::Config::default(),
        );
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), server_authzs)
                .instrument(info_span!("serverauthorizations")),
        );

        let authz_policies = guarded_watch::<k8s::policy::AuthorizationPolicy, _>(
            &mut runtime,
            watcher::Config::default(),
        );
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), authz_policies)
                .instrument(info_span!("authorizationpolicies")),
        );

        let mtls_authns = guarded_watch::<k8s::policy::MeshTLSAuthentication, _>(
            &mut runtime,
            watcher::Config::default(),
        );
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), mtls_authns)
                .instrument(info_span!("meshtlsauthentications")),
        );

        let network_authns = guarded_watch::<k8s::policy::NetworkAuthentication, _>(
            &mut runtime,
            watcher::Config::default(),
        );
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), network_authns)
                .instrument(info_span!("networkauthentications")),
        );

        let ratelimit_policies = guarded_watch::<k8s::policy::HttpLocalRateLimitPolicy, _>(
            &mut runtime,
            watcher::Config::default(),
        );
        let ratelimit_policies_indexes = IndexList::new(inbound_index.clone())
            .push(status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(ratelimit_policies_indexes.clone(), ratelimit_policies)
                .instrument(info_span!("httplocalratelimitpolicies")),
        );

        let http_routes_indexes = IndexList::new(inbound_index.clone())
            .push(outbound_index.clone())
            .push(status_index.clone())
            .shared();

        if api_resource_exists::<k8s::policy::HttpRoute>(&runtime.client()).await {
            let http_routes = guarded_watch::<k8s::policy::HttpRoute, _>(
                &mut runtime,
                watcher::Config::default(),
            );

            tokio::spawn(
                kubert::index::namespaced(http_routes_indexes.clone(), http_routes)
                    .instrument(info_span!("httproutes.policy.linkerd.io")),
            );
        } else {
            tracing::warn!(
                "httproutes.policy.linkerd.io resource kind not found, skipping watches"
            );
        }

        if api_resource_exists::<gateway::HTTPRoute>(&runtime.client()).await {
            let gateway_http_routes =
                guarded_watch::<gateway::HTTPRoute, _>(&mut runtime, watcher::Config::default());
            tokio::spawn(
                kubert::index::namespaced(http_routes_indexes, gateway_http_routes)
                    .instrument(info_span!("httproutes.gateway.networking.k8s.io")),
            );
        } else {
            tracing::warn!(
                "httproutes.gateway.networking.k8s.io resource kind not found, skipping watches"
            );
        }

        if api_resource_exists::<gateway::GRPCRoute>(&runtime.client()).await {
            let gateway_grpc_routes =
                guarded_watch::<gateway::GRPCRoute, _>(&mut runtime, watcher::Config::default());
            let gateway_grpc_routes_indexes = IndexList::new(outbound_index.clone())
                .push(inbound_index.clone())
                .push(status_index.clone())
                .shared();
            tokio::spawn(
                kubert::index::namespaced(gateway_grpc_routes_indexes.clone(), gateway_grpc_routes)
                    .instrument(info_span!("grpcroutes.gateway.networking.k8s.io")),
            );
        } else {
            tracing::warn!(
                "grpcroutes.gateway.networking.k8s.io resource kind not found, skipping watches"
            );
        }

        match tls_route_api_version {
            Some(version) => {
                let tls_routes_indexes = IndexList::new(status_index.clone())
                    .push(outbound_index.clone())
                    .shared();
                // Routes read from a `v1alpha2` watch are converted to their
                // `v1` representation so that a single index implementation
                // serves both versions.
                let tls_routes = match version {
                    gateway::TlsRouteApiVersion::V1 => guarded_watch::<gateway::TLSRoute, _>(
                        &mut runtime,
                        watcher::Config::default(),
                    )
                    .boxed(),
                    gateway::TlsRouteApiVersion::V1Alpha2 => {
                        guarded_watch::<gateway::TLSRouteV1Alpha2, _>(
                            &mut runtime,
                            watcher::Config::default(),
                        )
                        .map(convert_event)
                        .boxed()
                    }
                };
                tokio::spawn(
                    kubert::index::namespaced(tls_routes_indexes.clone(), tls_routes).instrument(
                        info_span!(
                            "tlsroutes.gateway.networking.k8s.io",
                            version = version.version()
                        ),
                    ),
                );
            }
            None => {
                tracing::warn!(
                    "tlsroutes.gateway.networking.k8s.io resource kind not found, skipping watches"
                );
            }
        }

        if api_resource_exists::<gateway::TCPRoute>(&runtime.client()).await {
            let tcp_routes =
                guarded_watch::<gateway::TCPRoute, _>(&mut runtime, watcher::Config::default());
            let tcp_routes_indexes = IndexList::new(status_index.clone())
                .push(outbound_index.clone())
                .shared();
            tokio::spawn(
                kubert::index::namespaced(tcp_routes_indexes.clone(), tcp_routes)
                    .instrument(info_span!("tcproutes.gateway.networking.k8s.io")),
            );
        } else {
            tracing::warn!(
                "tcproutes.gateway.networking.k8s.io resource kind not found, skipping watches"
            );
        }

        let services = guarded_watch::<k8s::Service, _>(&mut runtime, watcher::Config::default());
        let services_indexes = IndexList::new(outbound_index.clone())
            .push(status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(services_indexes, services)
                .instrument(info_span!("services")),
        );

        let egress_networks = guarded_watch::<k8s::policy::EgressNetwork, _>(
            &mut runtime,
            watcher::Config::default(),
        );
        let egress_networks_indexes = IndexList::new(status_index.clone())
            .push(outbound_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(egress_networks_indexes, egress_networks)
                .instrument(info_span!("egressnetworks")),
        );

        // Spawn the status Controller reconciliation.
        tokio::spawn(
            status::Index::run(status_index.clone(), RECONCILIATION_PERIOD)
                .instrument(info_span!("status_index")),
        );

        // Run the gRPC server, serving results by looking up against the index handle.
        tokio::spawn(grpc(
            grpc_addr,
            cluster_domain,
            cluster_networks,
            allow_l5d_request_headers,
            inbound_index,
            outbound_index,
            grpc_metrics.clone(),
            runtime.shutdown_handle(),
        ));

        let client = runtime.client();
        let status_controller = status::Controller::new(
            claims,
            client,
            hostname,
            updates_rx,
            Duration::from_millis(patch_timeout_ms),
            status_metrics,
            tls_route_api_version.unwrap_or_default(),
        );
        tokio::spawn(
            status_controller
                .run()
                .instrument(info_span!("status_controller")),
        );

        let runtime = runtime.spawn_server(Admission::new);

        // Run until the shutdown signal fires or the lease task ends unexpectedly.
        // Consumers of `claims` panic if the lease watch is ever dropped, on the
        // assumption that Kubernetes will restart the container (see
        // https://github.com/linkerd/linkerd2/pull/10584); watch the lease task here
        // so the process exits deterministically instead of relying on that panic
        // being observed from within a detached task.
        tokio::select! {
            biased;
            res = lease_task => {
                match res {
                    Ok(Ok(())) => {
                        bail!("Lease task exited unexpectedly");
                    }
                    Ok(Err(error)) => {
                        bail!("Lease task failed: {error}");
                    }
                    Err(error) if error.is_panic() => {
                        std::panic::resume_unwind(error.into_panic());
                    }
                    Err(error) => {
                        bail!("Lease task was cancelled: {error}");
                    }
                }
            }
            res = runtime.run() => {
                if res.is_err() {
                    bail!("Aborted");
                }
            }
        }

        Ok(())
    }
}

#[derive(Clone, Debug)]
struct IpNets(Vec<IpNet>);

impl std::str::FromStr for IpNets {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> Result<Self> {
        s.split(',')
            .map(|n| n.parse().map_err(Into::into))
            .collect::<Result<Vec<IpNet>>>()
            .map(Self)
    }
}

#[instrument(skip_all, fields(port = %addr.port()))]
#[allow(clippy::too_many_arguments)]
async fn grpc(
    addr: SocketAddr,
    cluster_domain: String,
    cluster_networks: Vec<IpNet>,
    allow_l5d_request_headers: bool,
    inbound_index: index::inbound::SharedIndex,
    outbound_index: index::outbound::SharedIndex,
    metrics: GrpcServerMetricsFamily,
    drain: drain::Watch,
) -> Result<()> {
    let inbound_discover = InboundDiscover::new(inbound_index);
    let inbound_svc = grpc::inbound::InboundPolicyServer::new(
        inbound_discover,
        cluster_networks,
        drain.clone(),
        metrics.clone(),
    )
    .svc();

    let outbound_discover = OutboundDiscover::new(outbound_index);
    let outbound_svc = grpc::outbound::OutboundPolicyServer::new(
        outbound_discover,
        cluster_domain,
        allow_l5d_request_headers,
        drain.clone(),
        metrics,
    )
    .svc();

    let (close_tx, close_rx) = tokio::sync::oneshot::channel();
    tokio::pin! {
        let srv = Server::builder().add_service(inbound_svc).add_service(outbound_svc).serve_with_shutdown(addr, close_rx.map(|_| {}));
    }

    info!(%addr, "policy gRPC server listening");
    tokio::select! {
        res = (&mut srv) => res?,
        handle = drain.signaled() => {
            let _ = close_tx.send(());
            handle.release_after(srv).await?
        }
    }
    Ok(())
}

async fn api_resource_exists<T>(client: &Client) -> bool
where
    T: Resource,
    T::DynamicType: Default,
{
    let dt = Default::default();
    api_version_has_kind(client, &T::api_version(&dt), &T::kind(&dt)).await
}

async fn api_version_has_kind(client: &Client, api_version: &str, kind: &str) -> bool {
    client
        .list_api_group_resources(api_version)
        .await
        .ok()
        .iter()
        .flat_map(|r| r.resources.iter())
        .any(|r| r.kind == kind)
}

/// Returns the most preferred version of the TLSRoute API that the cluster
/// serves, if any.
///
/// TLSRoute has no version that is served by every supported Gateway API
/// bundle: v1.2--v1.4 (and the CRDs vendored by the `linkerd-crds` chart) serve
/// `v1alpha2`, while the standard channel of v1.5 serves `v1` only.
async fn tls_route_api_version(client: &Client) -> Option<gateway::TlsRouteApiVersion> {
    let kind = gateway::TLSRoute::kind(&());
    for &version in gateway::TlsRouteApiVersion::ALL {
        if api_version_has_kind(client, version.api_version(), &kind).await {
            tracing::debug!(version = version.version(), "Found TLSRoute API version");
            return Some(version);
        }
    }
    None
}

/// Converts a watch event into one over an equivalent resource type.
fn convert_event<A, B: From<A>>(event: watcher::Event<A>) -> watcher::Event<B> {
    match event {
        watcher::Event::Apply(obj) => watcher::Event::Apply(obj.into()),
        watcher::Event::Delete(obj) => watcher::Event::Delete(obj.into()),
        watcher::Event::InitApply(obj) => watcher::Event::InitApply(obj.into()),
        watcher::Event::Init => watcher::Event::Init,
        watcher::Event::InitDone => watcher::Event::InitDone,
    }
}

// A watch that uses DeserializeGuard to skip resources which fail to deserialize.
// Any deserialization errors are logged as warnings and the event is skipped.
fn guarded_watch<R, T>(
    runtime: &mut kubert::Runtime<T>,
    watcher_config: watcher::Config,
) -> impl Stream<Item = watcher::Event<R>>
where
    R: Resource + DeserializeOwned + Clone + Debug + Send + 'static,
    R::DynamicType: Default,
{
    runtime
        .watch_all::<DeserializeGuard<R>>(watcher_config)
        .filter_map(async |item| {
            let log_warning = |t: &DeserializeGuard<R>| {
                if let Err(ref err) = t.0 {
                    tracing::warn!(
                        "skipping invalid {} resource {}/{}: {}",
                        R::kind(&Default::default()),
                        t.namespace().unwrap_or("<cluster>".to_string()),
                        t.name_any(),
                        err,
                    );
                }
            };
            match item {
                watcher::Event::Apply(t) => {
                    log_warning(&t);
                    t.0.ok().map(watcher::Event::Apply)
                }
                watcher::Event::Delete(t) => {
                    log_warning(&t);
                    t.0.ok().map(watcher::Event::Delete)
                }
                watcher::Event::Init => Some(watcher::Event::<R>::Init),
                watcher::Event::InitApply(t) => {
                    log_warning(&t);
                    t.0.ok().map(watcher::Event::InitApply)
                }
                watcher::Event::InitDone => Some(watcher::Event::<R>::InitDone),
            }
        })
}
