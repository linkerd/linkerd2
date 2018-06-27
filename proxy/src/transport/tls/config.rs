use std::{
    self,
    fs::File,
    io::{self, Cursor, Read},
    path::PathBuf,
    sync::Arc,
    time::Duration,
};

use super::{
    cert_resolver::CertResolver,
    Identity,

    rustls,
    untrusted,
    webpki,
};
use conditional::Conditional;
use futures::{future, stream, Future, Stream};
use futures_watch::Watch;
use ring::signature;

/// Not-yet-validated settings that are used for both TLS clients and TLS
/// servers.
///
/// The trust anchors are stored in PEM format because, in Kubernetes, they are
/// stored in a ConfigMap, and until very recently Kubernetes cannot store
/// binary data in ConfigMaps. Also, PEM is the most interoperable way to
/// distribute trust anchors, especially if it is desired to support multiple
/// trust anchors at once.
///
/// The end-entity certificate and private key are in DER format because they
/// are stored in the secret store where space utilization is a concern, and
/// because PEM doesn't offer any advantages.
#[derive(Clone, Debug)]
pub struct CommonSettings {
    /// The trust anchors as concatenated PEM-encoded X.509 certificates.
    pub trust_anchors: PathBuf,

    /// The end-entity certificate as a DER-encoded X.509 certificate.
    pub end_entity_cert: PathBuf,

    /// The private key in DER-encoded PKCS#8 form.
    pub private_key: PathBuf,

    /// The identity of the pod being proxied (as opposed to the psuedo-service
    /// exposed on the proxy's control port).
    pub pod_identity: Identity,

    /// The identity of the controller, if given.
    pub controller_identity: Conditional<Identity, ReasonForNoIdentity>,
}

/// Validated configuration common between TLS clients and TLS servers.
#[derive(Debug)]
struct CommonConfig {
    root_cert_store: rustls::RootCertStore,
    cert_resolver: Arc<CertResolver>,
}

/// Validated configuration for TLS servers.
#[derive(Clone)]
pub struct ClientConfig(pub(super) Arc<rustls::ClientConfig>);

/// XXX: `rustls::ClientConfig` doesn't implement `Debug` yet.
impl std::fmt::Debug for ClientConfig {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> Result<(), std::fmt::Error> {
        f.debug_struct("ClientConfig")
            .finish()
    }
}

/// Validated configuration for TLS servers.
#[derive(Clone)]
pub struct ServerConfig(pub(super) Arc<rustls::ServerConfig>);

pub type ClientConfigWatch = Watch<Conditional<ClientConfig, ReasonForNoTls>>;
pub type ServerConfigWatch = Watch<Conditional<ServerConfig, ReasonForNoTls>>;

/// The configuration in effect for a client (`ClientConfig`) or server
/// (`ServerConfig`) TLS connection.
#[derive(Clone, Debug)]
pub struct ConnectionConfig<C> where C: Clone {
    pub identity: Identity,
    pub config: C,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum ReasonForNoTls {
    /// TLS is disabled.
    Disabled,

    /// TLS was enabled but the configuration isn't available (yet).
    NoConfig,

    /// The endpoint's TLS identity is unknown. Without knowing its identity
    /// we can't validate its certificate.
    NoIdentity(ReasonForNoIdentity),

    /// The connection is between the proxy and the service
    InternalTraffic,

    /// The connection isn't TLS or it is TLS but not intended to be handled
    /// by the proxy.
    NotProxyTls,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum ReasonForNoIdentity {
    /// The connection is a non-HTTP connection so we don't know anything
    /// about the destination besides its address.
    NotHttp,

    /// The connection is for HTTP but the HTTP request doesn't have an
    /// authority so we can't extract the identity from it.
    NoAuthorityInHttpRequest,

    /// The destination service didn't give us the identity, which is its way
    /// of telling us that we shouldn't do TLS for this endpoint.
    NotProvidedByServiceDiscovery,

    /// The proxy wasn't configured with the identity.
    NotConfigured,

    /// We haven't implemented the mechanism to construct a TLs identity for
    /// the tap psuedo-service yet.
    NotImplementedForTap,

    /// We haven't implemented the mechanism to construct a TLs identity for
    /// the metrics psuedo-service yet.
    NotImplementedForMetrics,
}

impl From<ReasonForNoIdentity> for ReasonForNoTls {
    fn from(r: ReasonForNoIdentity) -> Self {
        ReasonForNoTls::NoIdentity(r)
    }
}

pub type ConditionalConnectionConfig<C> = Conditional<ConnectionConfig<C>, ReasonForNoTls>;
pub type ConditionalClientConfig = Conditional<ClientConfig, ReasonForNoTls>;

#[derive(Debug)]
pub enum Error {
    Io(PathBuf, io::Error),
    FailedToParseTrustAnchors(Option<webpki::Error>),
    EndEntityCertIsNotValid(rustls::TLSError),
    InvalidPrivateKey,
}

impl CommonSettings {
    fn paths(&self) -> [&PathBuf; 3] {
        [
            &self.trust_anchors,
            &self.end_entity_cert,
            &self.private_key,
        ]
    }

    /// Stream changes to the files described by this `CommonSettings`.
    ///
    /// The returned stream consists of each subsequent successfully loaded
    /// `CommonSettings` after each change. If the settings could not be
    /// reloaded (i.e., they were malformed), nothing is sent.
    fn stream_changes(self, interval: Duration)
        -> impl Stream<Item = CommonConfig, Error = ()>
    {
        let paths = self.paths().iter()
            .map(|&p| p.clone())
            .collect::<Vec<_>>();
        // Generate one "change" immediately before starting to watch
        // the files, so that we'll try to load them now if they exist.
        stream::once(Ok(()))
            .chain(::fs_watch::stream_changes(paths, interval))
            .filter_map(move |_| {
                CommonConfig::load_from_disk(&self)
                    .map_err(|e| warn!("error reloading TLS config: {:?}, falling back", e))
                    .ok()
            })
    }

}

impl CommonConfig {
    /// Loads a configuration from the given files and validates it. If an
    /// error is returned then the caller should try again after the files are
    /// updated.
    ///
    /// In a valid configuration, all the files need to be in sync with each
    /// other. For example, the private key file must contain the private
    /// key for the end-entity certificate, and the end-entity certificate
    /// must be issued by the CA represented by a certificate in the
    /// trust anchors file. Since filesystem operations are not atomic, we
    /// need to check for this consistency.
    fn load_from_disk(settings: &CommonSettings) -> Result<Self, Error> {
        let root_cert_store = load_file_contents(&settings.trust_anchors)
            .and_then(|file_contents| {
                let mut root_cert_store = rustls::RootCertStore::empty();
                let (added, skipped) = root_cert_store.add_pem_file(&mut Cursor::new(file_contents))
                    .map_err(|err| {
                        error!("error parsing trust anchors file: {:?}", err);
                        Error::FailedToParseTrustAnchors(None)
                    })?;
                if skipped != 0 {
                    warn!("skipped {} trust anchors in trust anchors file", skipped);
                }
                if added > 0 {
                    Ok(root_cert_store)
                } else {
                    error!("no valid trust anchors in trust anchors file");
                    Err(Error::FailedToParseTrustAnchors(None))
                }
            })?;

        let end_entity_cert = load_file_contents(&settings.end_entity_cert)?;

        // XXX: Assume there are no intermediates since there is no way to load
        // them yet.
        let cert_chain = vec![rustls::Certificate(end_entity_cert)];

        // Load the private key after we've validated the certificate.
        let private_key = load_file_contents(&settings.private_key)?;
        let private_key = untrusted::Input::from(&private_key);

        // Ensure the certificate is valid for the services we terminate for
        // TLS. This assumes that server cert validation does the same or
        // more validation than client cert validation.
        //
        // XXX: Rustls currently only provides access to a
        // `ServerCertVerifier` through
        // `rustls::ClientConfig::get_verifier()`.
        //
        // XXX: Once `rustls::ServerCertVerified` is exposed in Rustls's
        // safe API, remove the `map(|_| ())` below.
        //
        // TODO: Restrict accepted signatutre algorithms.
        let certificate_was_validated =
            rustls::ClientConfig::new().get_verifier().verify_server_cert(
                    &root_cert_store,
                    &cert_chain,
                    settings.pod_identity.as_dns_name_ref(),
                    &[]) // No OCSP
                .map(|_| ())
                .map_err(|err| {
                    error!("validating certificate failed for {:?}: {}", settings.pod_identity, err);
                    Error::EndEntityCertIsNotValid(err)
                })?;

        // `CertResolver::new` is responsible for verifying that the
        // private key is the right one for the certificate.
        let cert_resolver = CertResolver::new(certificate_was_validated, cert_chain, private_key)?;

        info!("loaded TLS configuration.");

        Ok(Self {
            root_cert_store,
            cert_resolver: Arc::new(cert_resolver),
        })
    }

}

// A Future that, when polled, checks for config updates and publishes them.
pub type PublishConfigs = Box<Future<Item = (), Error = ()> + Send>;

/// Returns Client and Server config watches, and a task to drive updates.
///
/// The returned task Future is expected to never complete.
///
/// If there are no TLS settings, then empty watches are returned. In this case, the
/// Future is never notified.
///
/// If all references are dropped to _either_ the client or server config watches, all
/// updates will cease for both config watches.
pub fn watch_for_config_changes(settings: Conditional<&CommonSettings, ReasonForNoTls>)
    -> (ClientConfigWatch, ServerConfigWatch, PublishConfigs)
{
    let settings = if let Conditional::Some(settings) = settings {
        settings.clone()
    } else {
        let (client_watch, _) = Watch::new(Conditional::None(ReasonForNoTls::Disabled));
        let (server_watch, _) = Watch::new(Conditional::None(ReasonForNoTls::Disabled));
        let no_future = future::empty();
        return (client_watch, server_watch, Box::new(no_future));
    };

    let changes = settings.stream_changes(Duration::from_secs(1));
    let (client_watch, client_store) = Watch::new(Conditional::None(ReasonForNoTls::NoConfig));
    let (server_watch, server_store) = Watch::new(Conditional::None(ReasonForNoTls::NoConfig));

    // `Store::store` will return an error iff all watchers have been dropped,
    // so we'll use `fold` to cancel the forwarding future. Eventually, we can
    // also use the fold to continue tracking previous states if we need to do
    // that.
    let f = changes
        .fold(
            (client_store, server_store),
            |(mut client_store, mut server_store), ref config| {
                client_store
                    .store(Conditional::Some(ClientConfig::from(config)))
                    .map_err(|_| trace!("all client config watchers dropped"))?;
                server_store
                    .store(Conditional::Some(ServerConfig::from(config)))
                    .map_err(|_| trace!("all server config watchers dropped"))?;
                Ok((client_store, server_store))
            })
        .then(|_| {
            error!("forwarding to tls config watches finished.");
            Err(())
        });

    // This function and `ServerConfig::no_tls` return `Box<Future<...>>`
    // rather than `impl Future<...>` so that they can have the _same_ return
    // types (impl Traits are not the same type unless the original
    // non-anonymized type was the same).
    (client_watch, server_watch, Box::new(f))
}

impl ClientConfig {
    fn from(common: &CommonConfig) -> Self {
        let mut config = rustls::ClientConfig::new();
        set_common_settings(&mut config.versions);

        // XXX: Rustls's built-in verifiers don't let us tweak things as fully
        // as we'd like (e.g. controlling the set of trusted signature
        // algorithms), but they provide good enough defaults for now.
        // TODO: lock down the verification further.
        // TODO: Change Rustls's API to Avoid needing to clone `root_cert_store`.
        config.root_store = common.root_cert_store.clone();

        // Disable session resumption for the time-being until resumption is
        // more tested.
        config.enable_tickets = false;

        // Enable client authentication if and only if we were configured for
        // it.
        config.client_auth_cert_resolver = common.cert_resolver.clone();

        ClientConfig(Arc::new(config))
    }

    /// Some tests aren't set up to do TLS yet, but we require a
    /// `ClientConfigWatch`. We can't use `#[cfg(test)]` here because the
    /// benchmarks use this.
    pub fn no_tls() -> ClientConfigWatch {
        let (watch, _) = Watch::new(Conditional::None(ReasonForNoTls::Disabled));
        watch
    }
}

impl ServerConfig {
    fn from(common: &CommonConfig) -> Self {
        // Ask TLS clients for a certificate and accept any certificate issued
        // by our trusted CA(s).
        //
        // Initially, also allow TLS clients that don't have a client
        // certificate too, to minimize risk. TODO: Use
        // `AllowAnyAuthenticatedClient` to require a valid certificate.
        //
        // XXX: Rustls's built-in verifiers don't let us tweak things as fully
        // as we'd like (e.g. controlling the set of trusted signature
        // algorithms), but they provide good enough defaults for now.
        // TODO: lock down the verification further.
        //
        // TODO: Change Rustls's API to Avoid needing to clone `root_cert_store`.
        let client_cert_verifier =
            rustls::AllowAnyAnonymousOrAuthenticatedClient::new(common.root_cert_store.clone());

        let mut config = rustls::ServerConfig::new(client_cert_verifier);
        set_common_settings(&mut config.versions);
        config.cert_resolver = common.cert_resolver.clone();
        ServerConfig(Arc::new(config))
    }
}

fn load_file_contents(path: &PathBuf) -> Result<Vec<u8>, Error> {
    fn load_file(path: &PathBuf) -> Result<Vec<u8>, io::Error> {
        let mut result = Vec::new();
        let mut file = File::open(path)?;
        loop {
            match file.read_to_end(&mut result) {
                Ok(_) => {
                    return Ok(result);
                },
                Err(e) => {
                    if e.kind() != io::ErrorKind::Interrupted {
                        return Err(e);
                    }
                },
            }
        }
    }

    load_file(path)
        .map(|contents| {
            trace!("loaded file {:?}", path);
            contents
        })
        .map_err(|e| Error::Io(path.clone(), e))
}

fn set_common_settings(versions: &mut Vec<rustls::ProtocolVersion>) {
    // Only enable TLS 1.2 until TLS 1.3 is stable.
    *versions = vec![rustls::ProtocolVersion::TLSv1_2]

    // XXX: Rustls doesn't provide a good way to customize the cipher suite
    // support, so just use its defaults, which are still pretty good.
    // TODO: Expand Rustls's API to allow us to clearly whitelist the cipher
    // suites we want to enable.
}

// Keep these in sync.
pub(super) static SIGNATURE_ALG_RING_SIGNING: &signature::SigningAlgorithm =
    &signature::ECDSA_P256_SHA256_ASN1_SIGNING;
pub(super) const SIGNATURE_ALG_RUSTLS_SCHEME: rustls::SignatureScheme =
    rustls::SignatureScheme::ECDSA_NISTP256_SHA256;
pub(super) const SIGNATURE_ALG_RUSTLS_ALGORITHM: rustls::internal::msgs::enums::SignatureAlgorithm =
    rustls::internal::msgs::enums::SignatureAlgorithm::ECDSA;

#[cfg(test)]
mod test_util {
    use std::path::PathBuf;

    use conditional::Conditional;
    use tls::{CommonSettings, Identity, ReasonForNoIdentity};

    pub struct Strings {
        pub identity: &'static str,
        pub trust_anchors: &'static str,
        pub end_entity_cert: &'static str,
        pub private_key: &'static str,
    }

    pub static FOO_NS1: Strings = Strings {
        identity: "foo.deployment.ns1.conduit-managed.conduit.svc.cluster.local",
        trust_anchors: "ca1.pem",
        end_entity_cert: "foo-ns1-ca1.crt",
        private_key: "foo-ns1-ca1.p8",
    };

    impl Strings {
        pub fn to_settings(&self) -> CommonSettings {
            let dir = PathBuf::from("src/transport/tls/testdata");
            CommonSettings {
                pod_identity: Identity::from_sni_hostname(self.identity.as_bytes()).unwrap(),
                controller_identity: Conditional::None(ReasonForNoIdentity::NotConfigured),
                trust_anchors: dir.join(self.trust_anchors),
                end_entity_cert: dir.join(self.end_entity_cert),
                private_key: dir.join(self.private_key),
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use tls::{ClientConfig, ServerConfig};
    use super::{CommonConfig, Error, test_util::*};

    #[test]
    fn can_construct_client_and_server_config_from_valid_settings() {
        let settings = FOO_NS1.to_settings();
        let common = CommonConfig::load_from_disk(&settings).unwrap();
        let _: ClientConfig = ClientConfig::from(&common); // infallible
        let _: ServerConfig = ServerConfig::from(&common); // infallible
    }

    #[test]
    fn recognize_ca_did_not_issue_cert() {
        let settings = Strings {
            trust_anchors: "ca2.pem",
            ..FOO_NS1
        }.to_settings();
        match CommonConfig::load_from_disk(&settings) {
            Err(Error::EndEntityCertIsNotValid(_)) => (),
            r => unreachable!("CommonConfig::load_from_disk returned {:?}", r),
        }
    }

    #[test]
    fn recognize_cert_is_not_valid_for_identity() {
        let settings = Strings {
            end_entity_cert: "bar-ns1-ca1.crt",
            private_key: "bar-ns1-ca1.p8",
            ..FOO_NS1
        }.to_settings();
        match CommonConfig::load_from_disk(&settings) {
            Err(Error::EndEntityCertIsNotValid(_)) => (),
            r => unreachable!("CommonConfig::load_from_disk returned {:?}", r),
        }
    }

    // XXX: The check that this tests hasn't been implemented yet.
    #[test]
    #[should_panic]
    fn recognize_private_key_is_not_valid_for_cert() {
        let settings = Strings {
            private_key: "bar-ns1-ca1.p8",
            ..FOO_NS1
        }.to_settings();
        match CommonConfig::load_from_disk(&settings) {
            Err(_) => (), // // TODO: Err(Error::InvalidPrivateKey) > (),
            r => unreachable!("CommonConfig::load_from_disk returned {:?}", r),
        }
    }
}
