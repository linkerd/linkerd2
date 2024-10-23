use linkerd_policy_controller_core::IpNet;
mod egress_network;
mod routes;

pub fn default_cluster_networks() -> Vec<IpNet> {
    vec![
        "10.0.0.0/8".parse().unwrap(),
        "100.64.0.0/10".parse().unwrap(),
        "172.16.0.0/12".parse().unwrap(),
        "192.168.0.0/16".parse().unwrap(),
        "fd00::/8".parse().unwrap(),
    ]
}
