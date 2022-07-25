use crate::DefaultPolicy;
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{bail, Context, Result};
use linkerd_policy_controller_k8s_api as k8s;
use std::num::NonZeroU16;

/// Holds pod metadata/config that can change.
#[derive(Debug, PartialEq)]
pub(crate) struct Meta {
    /// The pod's labels. Used by `Server` pod selectors.
    pub labels: k8s::Labels,

    // Pod-specific settings (i.e., derived from annotations).
    pub settings: Settings,
}

/// Per-pod settings, as configured by the pod's annotations.
#[derive(Debug, Default, PartialEq)]
pub(crate) struct Settings {
    pub require_id_ports: PortSet,
    pub opaque_ports: PortSet,
    pub default_policy: Option<DefaultPolicy>,
}

/// A `HashSet` specialized for ports.
///
/// Because ports are `u16` values, this type avoids the overhead of actually
/// hashing ports.
pub(crate) type PortSet =
    std::collections::HashSet<NonZeroU16, std::hash::BuildHasherDefault<PortHasher>>;

/// A `HashMap` specialized for ports.
///
/// Because ports are `NonZeroU16` values, this type avoids the overhead of
/// actually hashing ports.
pub(crate) type PortMap<V> =
    std::collections::HashMap<NonZeroU16, V, std::hash::BuildHasherDefault<PortHasher>>;

/// A hasher for ports.
///
/// Because ports are single `NonZeroU16` values, we don't have to hash them; we can just use
/// the integer values as hashes directly.
///
/// Borrowed from the proxy.
#[derive(Debug, Default)]
pub(crate) struct PortHasher(u16);

/// Gets the set of named ports with `protocol: TCP` from a pod spec.
pub(crate) fn port_names(spec: &Option<k8s::PodSpec>) -> HashMap<String, PortSet> {
    let mut port_names = HashMap::<String, PortSet>::default();
    if let Some(spec) = spec {
        for container in spec.containers.iter() {
            if let Some(ref ports) = container.ports {
                for port in ports {
                    if let None | Some("TCP") = port.protocol.as_deref() {
                        if let Ok(cp) =
                            u16::try_from(port.container_port).and_then(NonZeroU16::try_from)
                        {
                            if let Some(name) = &port.name {
                                port_names.entry(name.clone()).or_default().insert(cp);
                            }
                        }
                    }
                }
            }
        }
    }
    port_names
}

/// Gets the container probe ports for a Pod.
///
/// The result is a mapping for each probe port exposed by a container in the
/// Pod and the paths for which probes are expected.
pub(crate) fn get_http_probes(
    spec: &k8s::PodSpec,
    _port_names: &HashMap<String, PortSet>,
) -> PortMap<HashSet<String>> {
    let mut http_probes = PortMap::<HashSet<String>>::default();
    for container in spec.containers.iter() {
        let probes = (container.liveness_probe.iter())
            .chain(container.readiness_probe.iter())
            .chain(container.startup_probe.iter());
        for probe in probes {
            if let Some(ref http) = probe.http_get {
                let path = http
                    .path
                    .as_ref()
                    .expect("probe with httpGet should have a path field");
                match http.port {
                    k8s::IntOrString::Int(port) => {
                        if let Ok(port) = u16::try_from(port).and_then(NonZeroU16::try_from) {
                            let paths = http_probes.entry(port).or_default();
                            paths.insert(path.clone());
                        }
                    }
                    k8s::IntOrString::String(ref name) => {
                        for port in container.ports.iter().flatten() {
                            port.name.as_ref().map(|n| {
                                if n == name {
                                    if let Ok(port) = u16::try_from(port.container_port)
                                        .and_then(NonZeroU16::try_from)
                                    {
                                        let paths = http_probes.entry(port).or_default();
                                        paths.insert(path.clone());
                                    }
                                }
                            });
                        }
                    }
                }
            }
        }
    }
    http_probes
}

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
            tracing::warn!(%error, "invalid default policy annotation value");
            None
        });

        let opaque_ports = ports_annotation(anns, "config.linkerd.io/opaque-ports");
        let require_id_ports = ports_annotation(
            anns,
            "config.linkerd.io/proxy-require-identity-inbound-ports",
        );

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
fn ports_annotation(
    annotations: &std::collections::BTreeMap<String, String>,
    annotation: &str,
) -> PortSet {
    annotations
        .get(annotation)
        .map(|spec| {
            parse_portset(spec).unwrap_or_else(|error| {
                tracing::info!(%spec, %error, %annotation, "Invalid ports list");
                Default::default()
            })
        })
        .unwrap_or_default()
}

/// Read a comma-separated of ports or port ranges from the given string.
fn parse_portset(s: &str) -> Result<PortSet> {
    let mut ports = PortSet::default();

    for spec in s.split(',') {
        match spec.split_once('-') {
            None => {
                if !spec.trim().is_empty() {
                    let port = spec.trim().parse().context("parsing port")?;
                    ports.insert(port);
                }
            }
            Some((floor, ceil)) => {
                let floor = floor.trim().parse::<NonZeroU16>().context("parsing port")?;
                let ceil = ceil.trim().parse::<NonZeroU16>().context("parsing port")?;
                if floor > ceil {
                    bail!("Port range must be increasing");
                }
                ports.extend(
                    (u16::from(floor)..=u16::from(ceil)).map(|p| NonZeroU16::try_from(p).unwrap()),
                );
            }
        }
    }

    Ok(ports)
}

// === impl PortHasher ===

impl std::hash::Hasher for PortHasher {
    fn write(&mut self, _: &[u8]) {
        unreachable!("hashing a `u16` calls `write_u16`");
    }

    #[inline]
    fn write_u16(&mut self, port: u16) {
        self.0 = port;
    }

    #[inline]
    fn finish(&self) -> u64 {
        self.0 as u64
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use linkerd_policy_controller_k8s_api as k8s;

    macro_rules! ports {
        ($($x:expr),+ $(,)?) => (
            vec![$($x),+]
                .into_iter()
                .map(NonZeroU16::try_from)
                .collect::<Result<PortSet, _>>()
                .unwrap()
        );
    }

    #[test]
    fn parse_portset() {
        use super::parse_portset;

        assert!(parse_portset("").unwrap().is_empty(), "empty");
        assert!(parse_portset("0").is_err(), "0");
        assert_eq!(parse_portset("1").unwrap(), ports![1], "1");
        assert_eq!(parse_portset("1-3").unwrap(), ports![1, 2, 3], "1-2");
        assert_eq!(parse_portset("4,1-2").unwrap(), ports![1, 2, 4], "4,1-2");
        assert!(parse_portset("2-1").is_err(), "2-1");
        assert!(parse_portset("2-").is_err(), "2-");
        assert!(parse_portset("65537").is_err(), "65537");
    }

    #[test]
    fn gets_pod_ports() {
        let pod = k8s::Pod {
            metadata: k8s::ObjectMeta {
                namespace: Some("ns".to_string()),
                name: Some("pod".to_string()),
                ..Default::default()
            },
            spec: Some(k8s::PodSpec {
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
                            name: Some("named-port-1".to_string()),
                            container_port: 6543,
                            ..Default::default()
                        }]),
                        liveness_probe: Some(k8s::Probe {
                            http_get: Some(k8s::HTTPGetAction {
                                path: Some("/liveness-container-2".to_string()),
                                port: k8s::IntOrString::String("named-port-1".to_string()),
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
            }),
            ..k8s::Pod::default()
        };
        let port_names = port_names(&pod.spec);
        let probes = get_http_probes(&pod.spec.unwrap(), &port_names);

        let port_5432 = u16::try_from(5432).and_then(NonZeroU16::try_from).unwrap();
        let mut expected_5432 = HashSet::new();
        expected_5432.insert("/liveness-container-1".to_string());
        expected_5432.insert("/ready-container-1".to_string());
        assert!(probes.get(&port_5432).is_some());
        assert_eq!(*probes.get(&port_5432).unwrap(), expected_5432);

        let port_6543 = u16::try_from(6543).and_then(NonZeroU16::try_from).unwrap();
        let mut expected_6543 = HashSet::new();
        expected_6543.insert("/liveness-container-2".to_string());
        expected_6543.insert("/ready-container-2".to_string());
        assert!(probes.get(&port_6543).is_some());
        assert_eq!(*probes.get(&port_6543).unwrap(), expected_6543);
    }
}
