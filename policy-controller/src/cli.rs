use crate::{ClusterInfo, DefaultPolicy, IpNet};
use anyhow::Result;
use clap::Parser;
use std::{net::SocketAddr, sync::Arc};

use linkerd_policy_controller_k8s_index::ports::parse_portset;

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "Linkerd 2 policy controller")]
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
    pub grpc_addr: SocketAddr,

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
    pub(crate) policy_deployment_name: String,

    #[clap(long, default_value = "linkerd")]
    pub(crate) control_plane_namespace: String,

    /// Network CIDRs of all expected probes.
    #[clap(long)]
    probe_networks: Option<IpNets>,

    #[clap(long)]
    default_opaque_ports: String,
}

#[derive(Clone, Debug)]
struct IpNets(Vec<IpNet>);

// === impl Args ===

impl Args {
    /// Returns a [`kubert::Runtime`] configured by the CLI arguments.
    pub async fn runtime(&self) -> Result<kubert::Runtime<Option<kubert::server::Bound>>> {
        let server = if self.admission_controller_disabled {
            None
        } else {
            Some(self.server.clone())
        };

        let mut admin = self.admin.clone().into_builder();
        admin.with_default_prometheus();

        kubert::Runtime::builder()
            .with_log(self.log_level.clone(), self.log_format.clone())
            .with_admin(admin)
            .with_client(self.client.clone())
            .with_optional_server(server)
            .build()
            .await
            .map_err(Into::into)
    }

    /// Returns `ClusterInfo` as configured by the CLI arguments.
    pub fn cluster_info(&self) -> Result<Arc<ClusterInfo>> {
        let probe_networks = self
            .probe_networks
            .map(|IpNets(nets)| nets)
            .unwrap_or_default();

        let default_opaque_ports = parse_portset(&self.default_opaque_ports)?;
        Ok(Arc::new(ClusterInfo {
            networks: self.cluster_networks.0.clone(),
            identity_domain: self.identity_domain.clone(),
            control_plane_ns: self.control_plane_namespace.clone(),
            dns_domain: self.cluster_domain.clone(),
            default_policy: self.default_policy.clone(),
            default_detect_timeout: crate::DETECT_TIMEOUT,
            default_opaque_ports,
            probe_networks,
        }))
    }
}

impl std::str::FromStr for IpNets {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> Result<Self> {
        s.split(',')
            .map(|n| n.parse().map_err(Into::into))
            .collect::<Result<Vec<IpNet>>>()
            .map(Self)
    }
}
