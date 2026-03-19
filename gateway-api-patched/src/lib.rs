pub mod duration;
pub use duration::Duration;
pub mod apis;
pub use apis::standard::*;

#[cfg(feature = "experimental")]
pub use apis::experimental;

#[cfg(test)]
mod tests {
    use std::process::Command;

    use anyhow::Error;
    use hyper_util::client::legacy::Client as HTTPClient;
    use hyper_util::rt::TokioExecutor;
    use k8s_openapi::apimachinery::pkg::apis::meta::v1::{Condition, Time};
    use k8s_openapi::chrono::Utc;
    use kube::Client as KubeClient;
    use kube::api::{Patch, PatchParams, PostParams};
    use kube::config::{KubeConfigOptions, Kubeconfig};
    use kube::core::ObjectMeta;
    use kube::{Api, Config, CustomResourceExt, client::ConfigExt};
    use serde_json::json;
    use tower::BoxError;
    use tower::ServiceBuilder;
    use uuid::Uuid;

    use crate::{
        constants::{
            GatewayConditionReason, GatewayConditionType, ListenerConditionReason,
            ListenerConditionType,
        },
        gatewayclasses::{GatewayClass, GatewayClassSpec},
        gateways::{
            Gateway, GatewaySpec, GatewayStatus, GatewayStatusAddresses, GatewayStatusListeners,
        },
    };

    // -------------------------------------------------------------------------
    // Tests
    // -------------------------------------------------------------------------

    #[ignore]
    #[tokio::test]
    async fn deploy_gateway() -> Result<(), Error> {
        let (client, cluster) = get_client().await?;
        let info = client.apiserver_version().await?;

        println!(
            "kind cluster {} is running, server version: {}",
            cluster.name, info.git_version
        );

        let mut gwc = GatewayClass {
            metadata: ObjectMeta::default(),
            spec: GatewayClassSpec {
                controller_name: "test-controller".to_string(),
                description: None,
                parameters_ref: None,
            },
            status: None,
        };
        gwc.metadata.name = Some("test-gateway-class".to_string());
        gwc = Api::all(client.clone())
            .create(&PostParams::default(), &gwc)
            .await?;

        assert!(gwc.metadata.name.is_some());
        assert!(gwc.metadata.uid.is_some());

        let mut gw = Gateway {
            metadata: ObjectMeta::default(),
            spec: GatewaySpec {
                gateway_class_name: gwc
                    .metadata
                    .name
                    .ok_or(Error::msg("could not find GatewayClass name"))?,
                ..Default::default()
            },
            status: None,
        };
        gw.metadata.name = Some("test-gateway".to_string());
        gw = Api::default_namespaced(client.clone())
            .create(&PostParams::default(), &gw)
            .await?;

        assert!(gw.metadata.name.is_some());
        assert!(gw.metadata.uid.is_some());

        let mut gw_status = GatewayStatus::default();
        gw_status.addresses = Some(vec![GatewayStatusAddresses::default()]);
        gw_status.listeners = Some(vec![GatewayStatusListeners {
            name: "tcp".into(),
            attached_routes: 0,
            supported_kinds: vec![],
            conditions: vec![Condition {
                last_transition_time: Time(Utc::now()),
                message: "testing gateway".to_string(),
                observed_generation: Some(1),
                reason: ListenerConditionReason::Programmed.to_string(),
                status: "True".to_string(),
                type_: ListenerConditionType::Programmed.to_string(),
            }],
        }]);
        gw_status.conditions = Some(vec![Condition {
            last_transition_time: Time(Utc::now()),
            message: "testing gateway".to_string(),
            observed_generation: Some(1),
            reason: GatewayConditionReason::Programmed.to_string(),
            status: "True".to_string(),
            type_: GatewayConditionType::Programmed.to_string(),
        }]);

        gw = Api::default_namespaced(client)
            .patch_status(
                gw.metadata.name.clone().unwrap().as_str(),
                &PatchParams::default(),
                &Patch::Merge(json!({
                    "status": Some(gw_status)
                })),
            )
            .await?;

        assert!(gw.status.is_some());
        assert!(gw.status.clone().unwrap().addresses.is_some());
        assert!(gw.status.clone().unwrap().listeners.is_some());
        assert!(gw.status.clone().unwrap().conditions.is_some());

        Ok(())
    }

    // -------------------------------------------------------------------------
    // Test Utilities
    // -------------------------------------------------------------------------

    struct Cluster {
        name: String,
    }

    impl Drop for Cluster {
        fn drop(&mut self) {
            match delete_kind_cluster(&self.name) {
                Err(err) => panic!("failed to cleanup kind cluster {}: {}", self.name, err),
                Ok(()) => {}
            }
        }
    }

    async fn get_client() -> Result<(kube::Client, Cluster), Error> {
        let cluster = create_kind_cluster()?;
        let kubeconfig_yaml = get_kind_kubeconfig(&cluster.name)?;
        let kubeconfig = Kubeconfig::from_yaml(&kubeconfig_yaml)?;
        let config =
            Config::from_custom_kubeconfig(kubeconfig, &KubeConfigOptions::default()).await?;

        let https = config.rustls_https_connector()?;
        let http_client = HTTPClient::builder(TokioExecutor::new()).build(https);
        let service = ServiceBuilder::new()
            .layer(config.base_uri_layer())
            .option_layer(config.auth_layer()?)
            .map_err(BoxError::from)
            .service(http_client);

        let client = KubeClient::new(service, config.default_namespace);

        deploy_crds(client.clone()).await?;

        Ok((client, cluster))
    }

    async fn deploy_crds(client: kube::Client) -> Result<(), Error> {
        let mut gwc_crd = GatewayClass::crd();
        gwc_crd.metadata.annotations = Some(std::collections::BTreeMap::from([(
            "api-approved.kubernetes.io".to_string(),
            "https://github.com/kubernetes/enhancements/pull/1111".to_string(),
        )]));

        Api::all(client.clone())
            .create(&PostParams::default(), &gwc_crd)
            .await?;

        let mut gw_crd = Gateway::crd();
        gw_crd.metadata.annotations = Some(std::collections::BTreeMap::from([(
            "api-approved.kubernetes.io".to_string(),
            "https://github.com/kubernetes/enhancements/pull/1111".to_string(),
        )]));

        Api::all(client.clone())
            .create(&PostParams::default(), &gw_crd)
            .await?;

        Ok(())
    }

    fn create_kind_cluster() -> Result<Cluster, Error> {
        let cluster_name = Uuid::new_v4().to_string();

        let output = Command::new("kind")
            .arg("create")
            .arg("cluster")
            .arg("--name")
            .arg(&cluster_name)
            .output()?;

        if !output.status.success() {
            return Err(Error::msg(String::from_utf8(output.stderr)?));
        }

        Ok(Cluster { name: cluster_name })
    }

    fn delete_kind_cluster(cluster_name: &str) -> Result<(), Error> {
        let output = Command::new("kind")
            .arg("delete")
            .arg("cluster")
            .arg("--name")
            .arg(cluster_name)
            .output()?;

        if !output.status.success() {
            return Err(Error::msg(String::from_utf8(output.stderr)?));
        }

        Ok(())
    }

    fn get_kind_kubeconfig(cluster_name: &str) -> Result<String, Error> {
        let output = Command::new("kind")
            .arg("get")
            .arg("kubeconfig")
            .arg("--name")
            .arg(cluster_name)
            .output()?;

        if !output.status.success() {
            return Err(Error::msg(String::from_utf8(output.stderr)?));
        }

        Ok(String::from_utf8(output.stdout)?)
    }
}
