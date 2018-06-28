use conduit_proxy_controller_grpc;
use convert::TryFrom;
use super::{DnsName, InvalidDnsName, webpki};
use std::sync::Arc;

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
            Some(Strategy::K8sPodIdentity(i)) => {
                // XXX: If we don't know the controller's namespace or we don't
                // share the same controller then we won't be able to validate
                // the certificate yet. TODO: Support cross-controller
                // certificate validation and lock this down.
                if controller_namespace != Some(i.controller_ns.as_ref()) {
                    return Ok(None);
                }
                Self::from_sni_hostname(i.pod_identity.as_bytes()).map(Some)
            },
            None => Ok(None), // No TLS.
        }
    }

    pub fn from_sni_hostname(hostname: &[u8]) -> Result<Self, ()> {
        if hostname.last() == Some(&b'.') {
            return Err(()); // SNI hostnames are implicitly absolute.
        }
        DnsName::try_from(hostname)
            .map(|name| Identity(Arc::new(name)))
            .map_err(|InvalidDnsName| {
                error!("Invalid DNS name: {:?}", hostname);
                ()
            })
    }

    pub(super) fn as_dns_name_ref(&self) -> webpki::DNSNameRef {
        (self.0).0.as_ref()
    }
}
