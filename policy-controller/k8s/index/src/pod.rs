use crate::{index::PodSettings, DefaultPolicy, Index};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{bail, Context, Result};
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};
use std::collections::hash_map::Entry;

impl kubert::index::IndexNamespacedResource<k8s::Pod> for Index {
    fn apply(&mut self, pod: k8s::Pod) {
        let namespace = pod.namespace().unwrap();
        let name = pod.name();
        let settings = pod_settings(&pod.metadata);

        if let Err(error) = self.ns_or_default(namespace).apply_pod(
            name,
            pod.metadata.labels.into(),
            tcp_port_names(pod.spec),
            settings,
        ) {
            tracing::error!(%error, "Illegal pod update");
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Entry::Occupied(mut entry) = self.entry(namespace) {
            entry.get_mut().delete_pod(&*name);
            if entry.get().is_empty() {
                entry.remove();
            }
        }
    }
}

/// Gets the set of named TCP ports from a pod spec.
fn tcp_port_names(spec: Option<k8s::PodSpec>) -> HashMap<String, HashSet<u16>> {
    let mut port_names = HashMap::default();
    if let Some(spec) = spec {
        for container in spec.containers.into_iter() {
            if let Some(ports) = container.ports {
                for port in ports.into_iter() {
                    if let None | Some("TCP") = port.protocol.as_deref() {
                        if let Some(name) = port.name {
                            port_names
                                .entry(name)
                                .or_insert_with(HashSet::new)
                                .insert(port.container_port as u16);
                        }
                    }
                }
            }
        }
    }
    port_names
}

/// Reads pod settings from the pod metadata including:
///
/// - Opaque ports
/// - Ports that require identity
/// - The pod's default policy
fn pod_settings(meta: &k8s::ObjectMeta) -> PodSettings {
    let anns = match meta.annotations.as_ref() {
        None => return PodSettings::default(),
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

    PodSettings {
        default_policy,
        opaque_ports,
        require_id_ports,
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
) -> HashSet<u16> {
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
fn parse_portset(s: &str) -> Result<HashSet<u16>> {
    let mut ports = HashSet::new();

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
