use conduit_proxy_controller_grpc;
use convert::TryFrom;
use super::{DnsName, InvalidDnsName};
use std::sync::Arc;

/// An endpoint's identity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Identity(Arc<DnsName>);

impl Identity {
    /// Parses the given TLS identity, if provided.
    ///
    /// `controller_namespace` will be None if TLS is disabled.
    ///
    /// In the event of an error, the error is logged, so no detailed error
    /// information is returned.
    pub fn maybe_from(
        pb: conduit_proxy_controller_grpc::destination::TlsIdentity,
        controller_namespace: Option<&str>)
        -> Result<Option<Self>, ()>
    {
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

        use conduit_proxy_controller_grpc::destination::tls_identity::Strategy;
        match pb.strategy {
            Some(Strategy::K8sPodNamespace(i)) => {
                // Log any/any per-component errors before returning.
                let controller_ns_check = check_single_label(&i.controller_ns, "controller_ms");
                let pod_ns_check = check_single_label(&i.pod_ns, "pod_ns");
                let pod_name_check = check_single_label(&i.pod_name, "pod_name");
                if controller_ns_check.is_err() || pod_ns_check.is_err() || pod_name_check.is_err() {
                    return Err(());
                }

                // XXX: If we don't know the controller's namespace or we don't
                // share the same controller then we won't be able to validate
                // the certificate yet. TODO: Support cross-controller
                // certificate validation and lock this down.
                if controller_namespace != Some(i.controller_ns.as_ref()) {
                    return Ok(None)
                }

                // We reserve all names under a fake "managed-pods" service in
                // our namespace for identifying pods by name.
                let name = format!(
                    "{pod}.{pod_ns}.conduit-managed-pods.{controller_ns}.svc.cluster.local.",
                    pod = i.pod_name,
                    pod_ns = i.pod_ns,
                    controller_ns = i.controller_ns,
                );

                DnsName::try_from(&name)
                    .map(|name| Some(Identity(Arc::new(name))))
                    .map_err(|InvalidDnsName| {
                        error!("Invalid DNS name: {:?}", name);
                        ()
                    })
            },
            None => Ok(None), // No TLS.
        }
    }
}
