use prometheus_client::{
    collector::Collector,
    encoding::{DescriptorEncoder, EncodeMetric},
    metrics::{gauge::ConstGauge, MetricType},
    registry::Registry,
};

use super::SharedIndex;

#[derive(Debug)]
struct Instrumented(SharedIndex);

pub fn register(reg: &mut Registry, index: SharedIndex) {
    reg.register_collector(Box::new(Instrumented(index)));
}

impl Collector for Instrumented {
    fn encode(
        &self,
        mut encoder: DescriptorEncoder<'_>,
    ) -> std::prelude::v1::Result<(), std::fmt::Error> {
        let this = self.0.read();

        let mut meshtls_authn_encoder = encoder.encode_descriptor(
            "meshtls_authentication_index_size",
            "The number of MeshTLS authentications in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, auth) in &this.authentications.by_ns {
            let labels = [("namespace", ns.as_str())];
            let meshtls_authn = ConstGauge::new(auth.meshtls.len() as u32);
            let meshtls_authn_encoder = meshtls_authn_encoder.encode_family(&labels)?;
            meshtls_authn.encode(meshtls_authn_encoder)?;
        }

        let mut network_authn_encoder = encoder.encode_descriptor(
            "network_authentication_index_size",
            "The number of Network authentications in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, auth) in &this.authentications.by_ns {
            let labels = [("namespace", ns.as_str())];
            let network_authn = ConstGauge::new(auth.network.len() as u32);
            let network_authn_encoder = network_authn_encoder.encode_family(&labels)?;
            network_authn.encode(network_authn_encoder)?;
        }

        let mut pods_encoder = encoder.encode_descriptor(
            "pod_index_size",
            "The number of pods in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = [("namespace", ns.as_str())];
            let pods = ConstGauge::new(index.pods.by_name.len() as u32);
            let pods_encoder = pods_encoder.encode_family(&labels)?;
            pods.encode(pods_encoder)?;
        }

        let mut external_workloads_encoder = encoder.encode_descriptor(
            "external_workload_index_size",
            "The number of external workloads in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = [("namespace", ns.as_str())];
            let external_workloads = ConstGauge::new(index.external_workloads.by_name.len() as u32);
            let external_workloads_encoder = external_workloads_encoder.encode_family(&labels)?;
            external_workloads.encode(external_workloads_encoder)?;
        }

        let mut servers_encoder = encoder.encode_descriptor(
            "server_index_size",
            "The number of servers in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = [("namespace", ns.as_str())];
            let servers = ConstGauge::new(index.policy.servers.len() as u32);
            let servers_encoder = servers_encoder.encode_family(&labels)?;
            servers.encode(servers_encoder)?;
        }

        let mut server_authz_encoder = encoder.encode_descriptor(
            "server_authorization_index_size",
            "The number of server authorizations in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = [("namespace", ns.as_str())];
            let server_authz = ConstGauge::new(index.policy.server_authorizations.len() as u32);
            let server_authz_encoder = server_authz_encoder.encode_family(&labels)?;
            server_authz.encode(server_authz_encoder)?;
        }

        let mut authz_policies_encoder = encoder.encode_descriptor(
            "authorization_policy_index_size",
            "The number of authorization policies in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = [("namespace", ns.as_str())];
            let authz_policies = ConstGauge::new(index.policy.authorization_policies.len() as u32);
            let authz_policies_encoder = authz_policies_encoder.encode_family(&labels)?;
            authz_policies.encode(authz_policies_encoder)?;
        }

        let mut http_routes_encoder = encoder.encode_descriptor(
            "http_route_index_size",
            "The number of HTTP routes in index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = [("namespace", ns.as_str())];
            let http_routes = ConstGauge::new(index.policy.http_routes.len() as u32);
            let http_routes_encoder = http_routes_encoder.encode_family(&labels)?;
            http_routes.encode(http_routes_encoder)?;
        }
        Ok(())
    }
}
