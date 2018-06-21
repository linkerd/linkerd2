use conduit_proxy_controller_grpc;
use convert::TryFrom;
use super::{DnsName, InvalidDnsName, webpki};
use std::sync::Arc;
use config::Namespaces;

/// An endpoint's identity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Identity(pub(super) Arc<DnsName>);

impl Identity {
    /// Parses the given TLS identity, if provided.
    ///
    /// `controller_namespace` will be None if TLS is disabled.
    ///
    /// In the event of an error, the error is logged, so no detailed error
    /// information is returned.
    pub fn maybe_from_protobuf(
        controller_namespace: Option<&str>,
        pb: conduit_proxy_controller_grpc::destination::TlsIdentity)
        -> Result<Option<Self>, ()>
    {
        use conduit_proxy_controller_grpc::destination::tls_identity::Strategy;
        match pb.strategy {
            Some(Strategy::K8sPodNamespace(i)) => {
                // XXX: If we don't know the controller's namespace or we don't
                // share the same controller then we won't be able to validate
                // the certificate yet. TODO: Support cross-controller
                // certificate validation and lock this down.
                if controller_namespace != Some(i.controller_ns.as_ref()) {
                    return Ok(None);
                }

                let namespaces = Namespaces {
                    pod: i.pod_ns,
                    tls_controller: Some(i.controller_ns),
                };
                Self::try_from_pod_name(&namespaces, &i.pod_name).map(Some)
            },
            None => Ok(None), // No TLS.
        }
    }

    pub fn try_from_pod_name(namespaces: &Namespaces, pod_name: &str) -> Result<Self, ()> {
        // Verifies that the string doesn't contain '.' so that it is safe to
        // join it using '.' to try to form a DNS name. The rest of the DNS
        // name rules will be enforced by `DnsName::try_from`.
        fn check_single_label(value: &str, name: &str) -> Result<(), ()> {
            if !value.contains('.') {
                Ok(())
            } else {
                error!("Invalid {}: {:?}", name, value);
                Err(())
            }
        }

        let controller_ns = if let Some(controller_ns) = &namespaces.tls_controller {
            controller_ns
        } else {
            error!("controller namespace not provided");
            return Err(());
        };

        // Log any/any per-component errors before returning.
        let controller_ns_check = check_single_label(controller_ns, "controller namespace");
        let pod_ns_check = check_single_label(&namespaces.pod, "pod namespace");
        let pod_name_check = check_single_label(pod_name, "pod name");
        if controller_ns_check.is_err() || pod_ns_check.is_err() || pod_name_check.is_err() {
            return Err(());
        }

        // We reserve all names under a fake "managed-pods" service in
        // our namespace for identifying pods by name.
        let name = format!(
            "{pod}.{pod_ns}.conduit-managed-pods.{controller_ns}.svc.cluster.local.",
            pod = pod_name,
            pod_ns = &namespaces.pod,
            controller_ns = controller_ns,
        );

        DnsName::try_from(&name)
            .map(|name| Identity(Arc::new(name)))
            .map_err(|InvalidDnsName| {
                error!("Invalid DNS name: {:?}", name);
                ()
            })
    }

    pub(super) fn as_dns_name_ref(&self) -> webpki::DNSNameRef {
        (self.0).0.as_ref()
    }
}
