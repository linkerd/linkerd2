use linkerd_policy_controller_k8s_api as k8s;

#[derive(Default)]
pub(crate) struct Service {
    cluster_ip: Option<String>,
    type_: Option<String>,
}

impl Service {
    pub(crate) fn valid_parent_service(&self) -> bool {
        let cluster_ip = self
            .cluster_ip
            .as_ref()
            .filter(|cip| !cip.eq_ignore_ascii_case("none"))
            .is_some();
        let external_name = self.type_.as_deref() == Some("ExternalName");
        cluster_ip && !external_name
    }
}

impl From<k8s::Service> for Service {
    fn from(svc: k8s::Service) -> Self {
        svc.spec
            .map(|spec| Self {
                cluster_ip: spec.cluster_ip,
                type_: spec.type_,
            })
            .unwrap_or_default()
    }
}
