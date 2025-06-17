use crate::defaults::DefaultPolicy;
use crate::ports::{parse_portset, PortMap, PortSet};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use linkerd_policy_controller_k8s_api as k8s;
use std::{collections::BTreeSet, num::NonZeroU16};

/// Holds workload metadata/config that can change.
#[derive(Debug, PartialEq)]
pub(crate) struct Meta {
    /// The workload's labels. Used by `Server` selectors.
    pub labels: k8s::Labels,

    // Workload-specific settings (i.e., derived from annotations).
    pub settings: Settings,
}

/// Per-workload settings, as configured by the workload's annotations.
#[derive(Debug, Default, PartialEq)]
pub(crate) struct Settings {
    pub require_id_ports: PortSet,
    pub opaque_ports: PortSet,
    pub default_policy: Option<DefaultPolicy>,
}

/// Gets the set of named ports with `protocol: TCP` from a pod spec.
pub(crate) fn pod_tcp_ports_by_name(spec: &k8s::PodSpec) -> HashMap<String, PortSet> {
    let mut ports = HashMap::<String, PortSet>::default();
    for (port, name) in spec
        .containers
        .iter()
        .flat_map(|c| c.ports.iter().flatten())
        .filter_map(named_tcp_port)
    {
        ports.entry(name.to_string()).or_default().insert(port);
    }
    ports
}

/// Gets the set of named ports withn `protocol: TCP` from an external workload
/// spec.
///
/// Since an external workload has only one set of ports, each name is
/// guaranteed to be unique.
pub(crate) fn external_tcp_ports_by_name(
    spec: &k8s::external_workload::ExternalWorkloadSpec,
) -> HashMap<String, NonZeroU16> {
    let mut ports = HashMap::<String, NonZeroU16>::default();
    for (port, name) in spec
        .ports
        .iter()
        .flatten()
        .filter_map(named_external_tcp_port)
    {
        ports.insert(name.into(), port);
    }
    ports
}

/// Gets the container probe ports for a Pod.
///
/// The result is a mapping for each probe port exposed by a container in the
/// Pod and the paths for which probes are expected.
pub(crate) fn pod_http_probes(pod: &k8s::PodSpec) -> PortMap<BTreeSet<String>> {
    let mut probes = PortMap::<BTreeSet<String>>::default();
    for (port, path) in pod
        .containers
        .iter()
        .chain(pod.init_containers.iter().flatten())
        .flat_map(container_http_probe_paths)
    {
        println!("inserting path {path} for port {port}");
        probes.entry(port).or_default().insert(path);
    }
    probes
}

fn container_http_probe_paths(
    container: &k8s::Container,
) -> impl Iterator<Item = (NonZeroU16, String)> + '_ {
    fn find_by_name(name: &str, ports: &[k8s::ContainerPort]) -> Option<NonZeroU16> {
        for (p, n) in ports.iter().filter_map(named_tcp_port) {
            if n.eq_ignore_ascii_case(name) {
                return Some(p);
            }
        }
        None
    }

    fn get_port(port: &k8s::IntOrString, container: &k8s::Container) -> Option<NonZeroU16> {
        match port {
            k8s::IntOrString::Int(p) => u16::try_from(*p).ok()?.try_into().ok(),
            k8s::IntOrString::String(n) => find_by_name(n, container.ports.as_ref()?),
        }
    }

    (container.liveness_probe.iter())
        .chain(container.readiness_probe.iter())
        .chain(container.startup_probe.iter())
        .filter_map(|p| {
            let probe = p.http_get.as_ref()?;
            let port = get_port(&probe.port, container)?;
            let path = probe.path.as_deref().unwrap_or("/");
            match http::Uri::try_from(path) {
                Ok(uri) => Some((port, uri.path().to_string())),
                Err(error) => {
                    tracing::warn!(%error, path, "Invalid probe path");
                    None
                }
            }
        })
}

fn named_tcp_port(port: &k8s::ContainerPort) -> Option<(NonZeroU16, &str)> {
    if let Some(ref proto) = port.protocol {
        if !proto.eq_ignore_ascii_case("TCP") {
            return None;
        }
    }
    let p = u16::try_from(port.container_port)
        .and_then(NonZeroU16::try_from)
        .ok()?;
    let n = port.name.as_deref()?;
    Some((p, n))
}

fn named_external_tcp_port(spec: &k8s::external_workload::PortSpec) -> Option<(NonZeroU16, &str)> {
    if let Some(ref proto) = spec.protocol {
        if !proto.eq_ignore_ascii_case("TCP") {
            return None;
        }
    }
    let n = spec.name.as_deref()?;
    Some((spec.port, n))
}

// === impl Meta ===

impl Meta {
    pub(crate) fn from_metadata(meta: k8s::ObjectMeta) -> Self {
        let settings = Settings::from_metadata(&meta);
        tracing::trace!(?settings);
        Self {
            settings,
            labels: meta.labels.into(),
        }
    }
}

// === impl Settings ===

impl Settings {
    /// Reads pod settings from the pod metadata including:
    ///
    /// - Opaque ports
    /// - Ports that require identity
    /// - The pod's default policy
    pub(crate) fn from_metadata(meta: &k8s::ObjectMeta) -> Self {
        let anns = match meta.annotations.as_ref() {
            None => return Self::default(),
            Some(anns) => anns,
        };

        let default_policy = default_policy(anns).unwrap_or_else(|error| {
            tracing::warn!(%error, "Invalid default policy annotation value");
            None
        });

        let opaque_ports =
            ports_annotation(anns, "config.linkerd.io/opaque-ports").unwrap_or_default();
        let require_id_ports = ports_annotation(
            anns,
            "config.linkerd.io/proxy-require-identity-inbound-ports",
        )
        .unwrap_or_default();

        Self {
            default_policy,
            opaque_ports,
            require_id_ports,
        }
    }
}

/// Attempts to read a default policy override from an annotation map.
fn default_policy(
    ann: &std::collections::BTreeMap<String, String>,
) -> Result<Option<DefaultPolicy>> {
    if let Some(v) = ann.get("config.linkerd.io/default-inbound-policy") {
        let mode = v.parse()?;
        return Ok(Some(mode));
    }

    Ok(None)
}

/// Reads `annotation` from the provided set of annotations, parsing it as a port set.  If the
/// annotation is not set or is invalid, the empty set is returned.
pub(crate) fn ports_annotation(
    annotations: &std::collections::BTreeMap<String, String>,
    annotation: &str,
) -> Option<PortSet> {
    annotations.get(annotation).map(|spec| {
        parse_portset(spec).unwrap_or_else(|error| {
            tracing::info!(%spec, %error, %annotation, "Invalid ports list");
            Default::default()
        })
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use linkerd_policy_controller_k8s_api as k8s;

    #[test]
    fn probe_multiple_paths() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![
                k8s::Container {
                    liveness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/liveness-container-1".to_string()),
                            port: k8s::IntOrString::Int(5432),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    readiness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/ready-container-1".to_string()),
                            port: k8s::IntOrString::Int(5432),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
                k8s::Container {
                    ports: Some(vec![k8s::ContainerPort {
                        name: Some("named-1".to_string()),
                        container_port: 6543,
                        ..Default::default()
                    }]),
                    liveness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/liveness-container-2".to_string()),
                            port: k8s::IntOrString::String("named-1".to_string()),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    readiness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/ready-container-2".to_string()),
                            port: k8s::IntOrString::Int(6543),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
            ],
            ..Default::default()
        });

        let port_5432 = u16::try_from(5432).and_then(NonZeroU16::try_from).unwrap();
        let mut expected_5432 = BTreeSet::new();
        expected_5432.insert("/liveness-container-1".to_string());
        expected_5432.insert("/ready-container-1".to_string());
        assert!(probes.contains_key(&port_5432));
        assert_eq!(*probes.get(&port_5432).unwrap(), expected_5432);

        let port_6543 = u16::try_from(6543).and_then(NonZeroU16::try_from).unwrap();
        let mut expected_6543 = BTreeSet::new();
        expected_6543.insert("/liveness-container-2".to_string());
        expected_6543.insert("/ready-container-2".to_string());
        assert!(probes.contains_key(&port_6543));
        assert_eq!(*probes.get(&port_6543).unwrap(), expected_6543);
    }

    #[test]
    fn probe_ignores_udp() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![k8s::Container {
                ports: Some(vec![k8s::ContainerPort {
                    container_port: 6543,
                    name: Some("named".to_string()),
                    protocol: Some("UDP".to_string()),
                    ..Default::default()
                }]),
                liveness_probe: Some(k8s::Probe {
                    http_get: Some(k8s::HTTPGetAction {
                        port: k8s::IntOrString::String("named".to_string()),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }],
            ..Default::default()
        });

        assert!(probes.is_empty());
    }

    #[test]
    fn probe_only_references_within_container() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![
                k8s::Container {
                    liveness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            port: k8s::IntOrString::String("named".to_string()),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
                k8s::Container {
                    ports: Some(vec![k8s::ContainerPort {
                        container_port: 6543,
                        name: Some("named".to_string()),
                        protocol: Some("TCP".to_string()),
                        ..Default::default()
                    }]),
                    ..Default::default()
                },
            ],
            ..Default::default()
        });

        assert!(probes.is_empty());
    }

    #[test]
    fn probe_ports_optional() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![k8s::Container {
                liveness_probe: Some(k8s::Probe {
                    http_get: Some(k8s::HTTPGetAction {
                        port: k8s::IntOrString::Int(8080),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }],
            ..Default::default()
        });

        assert_eq!(probes.len(), 1);
        let paths = probes.get(&8080.try_into().unwrap()).unwrap();
        assert_eq!(paths.len(), 1);
        assert_eq!(paths.iter().next().unwrap(), "/");
    }

    #[test]
    fn probe_with_params() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![k8s::Container {
                liveness_probe: Some(k8s::Probe {
                    http_get: Some(k8s::HTTPGetAction {
                        path: Some("/liveness-container-1?foo=bar".to_string()),
                        port: k8s::IntOrString::Int(5432),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }],
            ..Default::default()
        });

        assert_eq!(probes.len(), 1);
        let paths = probes.get(&5432.try_into().unwrap()).unwrap();
        assert_eq!(paths.len(), 1);
        assert_eq!(paths.iter().next().unwrap(), "/liveness-container-1");
    }
}
