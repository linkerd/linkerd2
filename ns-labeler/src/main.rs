use anyhow::{anyhow, Result};
use clap::Parser;
use json_patch::{AddOperation, PatchOperation::Add};
use k8s_openapi::api::core::v1::{ConfigMap, Namespace};
use kube::{
    api::{Api, Patch, PatchParams, ResourceExt},
    Client,
};
use serde_yaml::Value;
use thiserror::Error;
use tokio::time;
use tracing::{debug, info};

/// Add metadata to extension namespaces
///
/// The added metadata is used by the Linkerd CLI to recognize the extensions installed in the
/// cluster.
/// Note that this is only required when installing extensions via Helm.
#[derive(Parser)]
#[clap(version, about)]
struct Args {
    #[clap(long, env = "LINKERD_NS_LABELER_LOG_LEVEL", default_value = "info")]
    log_level: kubert::LogFilter,

    #[clap(long, env = "LINKERD_NS_LABELER_LOG_FORMAT", default_value = "plain")]
    log_format: kubert::LogFormat,

    /// Extension name (e.g. viz, multicluster, jaeger)
    #[clap(long)]
    extension: String,

    /// Namespace where the extension is installed
    #[clap(long, short = 'n')]
    namespace: String,

    /// Namespace where the Linkerd control-plane is installed
    #[clap(long)]
    linkerd_namespace: String,

    /// URL of external Prometheus instance, if any (only used by the viz
    /// extension)
    #[clap(long)]
    prometheus_url: Option<String>,
}

#[derive(Debug, Error)]
enum Error {
    #[error("data not found")]
    DataNotFound,
    #[error("values not found")]
    ValuesNotFound,
}

struct PatchOpts<'a> {
    extension: &'a str,
    prometheus_url: Option<String>,
    cni_enabled: bool,
    add_annotations_root: bool,
    add_labels_root: bool,
}

const LINKERD_CONFIG_CM: &str = "linkerd-config";
const WRITE_TIMEOUT: time::Duration = time::Duration::from_secs(10);
const FIELD_MANAGER: &str = "kubectl-label";

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    let Args {
        log_level,
        log_format,
        extension,
        namespace,
        linkerd_namespace,
        prometheus_url,
    } = Args::parse();

    log_format
        .try_init(log_level)
        .expect("must configure logging");

    info!("patching namespace {}", namespace);

    let client = Client::try_default().await?;
    let namespaces = Api::<Namespace>::all(client.clone());
    let ns = namespaces.get(&namespace).await?;

    let add_annotations_root = ns.annotations().is_empty();
    let add_labels_root = ns.labels().is_empty();
    let cni_enabled = get_cni_enabled(client, &linkerd_namespace).await?;
    let ops = PatchOpts {
        extension: &extension,
        prometheus_url,
        cni_enabled,
        add_annotations_root,
        add_labels_root,
    };
    let patch = ops.create_patch();
    let params: PatchParams = PatchParams::apply(FIELD_MANAGER);
    time::timeout(
        WRITE_TIMEOUT,
        namespaces.patch(namespace.as_str(), &params, &Patch::Json::<()>(patch)),
    )
    .await?
    .map(|_| info!("successfully patched namespace"))
    .map_err(|e| {
        tracing::error!("failed patching namespace: {}", e);
        anyhow!("failed patching namespace")
    })
}

async fn get_cni_enabled(client: Client, ns: &str) -> Result<bool> {
    let cm_api = Api::<ConfigMap>::namespaced(client, ns);
    let cm = cm_api.get(LINKERD_CONFIG_CM).await?;
    let data = cm.data.ok_or(Error::DataNotFound)?;
    let values = data.get("values").ok_or(Error::ValuesNotFound)?;
    let values_yaml = serde_yaml::from_str(&values)?;
    match values_yaml {
        Value::Mapping(mapping) => match mapping.get(&Value::String("cniEnabled".to_string())) {
            Some(Value::Bool(true)) => Ok(true),
            _ => Ok(false),
        },
        _ => Ok(false),
    }
}

impl<'a> PatchOpts<'a> {
    fn create_patch(&self) -> json_patch::Patch {
        let mut patches = Vec::new();

        if self.add_annotations_root {
            patches.push(Add(AddOperation {
                path: "/metadata/annotations".to_string(),
                value: serde_json::json!({}),
            }));
        }

        if self.add_labels_root {
            patches.push(Add(AddOperation {
                path: "/metadata/labels".to_string(),
                value: serde_json::json!({}),
            }));
        }

        self.prometheus_url.as_deref().into_iter().for_each(|url| {
            patches.push(Add(AddOperation {
                path: "/metadata/annotations/viz.linkerd.io~1external-prometheus".to_string(),
                value: serde_json::Value::String(url.to_string()),
            }));
        });

        patches.push(Add(AddOperation {
            path: "/metadata/labels/linkerd.io~1extension".to_string(),
            value: serde_json::Value::String(self.extension.to_string()),
        }));

        let level = if self.cni_enabled {
            "restricted"
        } else {
            "privileged"
        };

        patches.push(Add(AddOperation {
            path: "/metadata/labels/pod-security.kubernetes.io~1enforce".to_string(),
            value: serde_json::Value::String(level.to_string()),
        }));

        debug!("patch to apply: {:?}", patches);

        json_patch::Patch(patches)
    }
}

#[cfg(test)]
mod tests {
    use crate::PatchOpts;
    use anyhow::Result;

    #[test]
    fn multicluster() -> Result<()> {
        let ops = PatchOpts {
            extension: "multicluster",
            prometheus_url: None,
            cni_enabled: false,
            add_annotations_root: false,
            add_labels_root: true,
        };
        let patch = ops.create_patch();
        let patch_str = serde_json::to_string(&patch)?;
        assert_eq!(
            patch_str,
            r#"
[
    {
        "op": "add",
        "path": "/metadata/labels",
        "value": {}
    },
    {
        "op": "add",
        "path": "/metadata/labels/linkerd.io~1extension",
        "value": "multicluster"
    },
    {
        "op": "add",
        "path": "/metadata/labels/pod-security.kubernetes.io~1enforce",
        "value": "privileged"
    }
]
"#
            .replace("\n", "")
            .replace(" ", "")
        );
        Ok(())
    }

    #[test]
    fn viz() -> Result<()> {
        let ops = PatchOpts {
            extension: "viz",
            prometheus_url: Some("prometheus.obs.svc.cluster.local:9090".to_string()),
            cni_enabled: true,
            add_annotations_root: true,
            add_labels_root: false,
        };
        let patch = ops.create_patch();
        let patch_str = serde_json::to_string(&patch)?;
        assert_eq!(
            patch_str,
            r#"
[
    {
        "op": "add",
        "path": "/metadata/annotations",
        "value": {}
    },
    {
        "op": "add",
        "path": "/metadata/annotations/viz.linkerd.io~1external-prometheus",
        "value": "prometheus.obs.svc.cluster.local:9090"
    },
    {
        "op": "add",
        "path": "/metadata/labels/linkerd.io~1extension",
        "value": "viz"
    },
    {
        "op": "add",
        "path": "/metadata/labels/pod-security.kubernetes.io~1enforce",
        "value": "restricted"
    }
]
"#
            .replace("\n", "")
            .replace(" ", "")
        );
        Ok(())
    }
}
