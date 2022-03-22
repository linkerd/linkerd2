use crate::DefaultPolicy;
use ahash::AHashMap as HashMap;
use anyhow::{bail, Context, Result};
use linkerd_policy_controller_k8s_api as k8s;

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
pub(crate) type PortSet = std::collections::HashSet<u16, std::hash::BuildHasherDefault<PortHasher>>;

/// A `HashMap` specialized for ports.
///
/// Because ports are `u16` values, this type avoids the overhead of actually
/// hashing ports.
pub(crate) type PortMap<V> =
    std::collections::HashMap<u16, V, std::hash::BuildHasherDefault<PortHasher>>;

/// A hasher for ports.
///
/// Because ports are single `u16` values, we don't have to hash them; we can just use
/// the integer values as hashes directly.
///
/// Borrowed from the proxy.
#[derive(Default)]
pub(crate) struct PortHasher(u16);

/// Gets the set of named TCP ports from a pod spec.
pub(crate) fn tcp_port_names(spec: Option<k8s::PodSpec>) -> HashMap<String, PortSet> {
    let mut port_names = HashMap::<String, PortSet>::default();
    if let Some(spec) = spec {
        for container in spec.containers.into_iter() {
            if let Some(ports) = container.ports {
                for port in ports.into_iter() {
                    if let None | Some("TCP") = port.protocol.as_deref() {
                        if let Some(name) = port.name {
                            port_names
                                .entry(name)
                                .or_default()
                                .insert(port.container_port as u16);
                        }
                    }
                }
            }
        }
    }
    port_names
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
                    if port == 0 {
                        bail!("port must not be 0")
                    }
                    ports.insert(port);
                }
            }
            Some((floor, ceil)) => {
                let floor = floor.trim().parse::<u16>().context("parsing port")?;
                let ceil = ceil.trim().parse::<u16>().context("parsing port")?;
                if floor == 0 {
                    bail!("port must not be 0")
                }
                if floor > ceil {
                    bail!("Port range must be increasing");
                }
                ports.extend(floor..=ceil);
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
    #[test]
    fn parse_portset() {
        use super::parse_portset;

        assert!(parse_portset("").unwrap().is_empty(), "empty");
        assert!(parse_portset("0").is_err(), "0");
        assert_eq!(
            parse_portset("1").unwrap(),
            vec![1].into_iter().collect(),
            "1"
        );
        assert_eq!(
            parse_portset("1-2").unwrap(),
            vec![1, 2].into_iter().collect(),
            "1-2"
        );
        assert_eq!(
            parse_portset("4,1-2").unwrap(),
            vec![1, 2, 4].into_iter().collect(),
            "4,1-2"
        );
        assert!(parse_portset("2-1").is_err(), "2-1");
        assert!(parse_portset("2-").is_err(), "2-");
        assert!(parse_portset("65537").is_err(), "65537");
    }
}
