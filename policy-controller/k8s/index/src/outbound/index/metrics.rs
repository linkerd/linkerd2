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
    fn encode(&self, mut encoder: DescriptorEncoder<'_>) -> Result<(), std::fmt::Error> {
        let this = self.0.read();

        let service_encoder = encoder.encode_descriptor(
            "service_index_size",
            "The number of entires in service index",
            None,
            MetricType::Gauge,
        )?;
        let services = ConstGauge::new(this.services_by_ip.len() as u32);
        services.encode(service_encoder)?;

        let service_info_encoder = encoder.encode_descriptor(
            "service_info_index_size",
            "The number of entires in the service info index",
            None,
            MetricType::Gauge,
        )?;
        let service_infos = ConstGauge::new(this.resource_info.len() as u32);
        service_infos.encode(service_info_encoder)?;

        let mut service_route_encoder = encoder.encode_descriptor(
            "service_route_index_size",
            "The number of entires in the service route index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = vec![("namespace", ns.as_str())];
            let service_routes = ConstGauge::new(index.service_http_routes.len() as u32);
            let service_route_encoder = service_route_encoder.encode_family(&labels)?;
            service_routes.encode(service_route_encoder)?;
        }

        let mut service_port_route_encoder = encoder.encode_descriptor(
            "service_port_route_index_size",
            "The number of entires in the service port route index",
            None,
            MetricType::Gauge,
        )?;
        for (ns, index) in &this.namespaces.by_ns {
            let labels = vec![("namespace", ns.as_str())];
            let service_port_routes = ConstGauge::new(index.resource_port_routes.len() as u32);
            let service_port_route_encoder = service_port_route_encoder.encode_family(&labels)?;
            service_port_routes.encode(service_port_route_encoder)?;
        }

        Ok(())
    }
}
