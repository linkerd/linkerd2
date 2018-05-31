use std::{
    fs::File,
    io::{self, Cursor, Read},
    path::PathBuf,
    sync::Arc,
};

use super::{
    cert_resolver::CertResolver,

    rustls,
    untrusted,
    webpki,
};

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
#[derive(Debug)]
pub struct CommonSettings {
    /// The trust anchors as concatenated PEM-encoded X.509 certificates.
    pub trust_anchors: PathBuf,

    /// The end-entity certificate as a DER-encoded X.509 certificate.
    pub end_entity_cert: PathBuf,

    /// The private key in DER-encoded PKCS#8 form.
    pub private_key: PathBuf,
}

/// Validated configuration common between TLS clients and TLS servers.
pub struct CommonConfig {
    cert_resolver: Arc<CertResolver>,
}

/// Validated configuration for TLS servers.
#[derive(Clone)]
pub struct ServerConfig(pub(super) Arc<rustls::ServerConfig>);

#[derive(Debug)]
pub enum Error {
    Io(PathBuf, io::Error),
    FailedToParsePrivateKey,
    FailedToParseTrustAnchors(Option<webpki::Error>),
    EmptyEndEntityCert,
    EndEntityCertIsNotValid(webpki::Error),
    InvalidPrivateKey,
    TimeConversionFailed,
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
    pub fn load_from_disk(settings: &CommonSettings) -> Result<Self, Error> {
        let trust_anchor_certs = load_file_contents(&settings.trust_anchors)
            .and_then(|file_contents|
                rustls::internal::pemfile::certs(&mut Cursor::new(file_contents))
                    .map_err(|()| Error::FailedToParseTrustAnchors(None)))?;
        let mut trust_anchors = Vec::with_capacity(trust_anchor_certs.len());
        for ta in &trust_anchor_certs {
            let ta = webpki::trust_anchor_util::cert_der_as_trust_anchor(
                untrusted::Input::from(ta.as_ref()))
                .map_err(|e| Error::FailedToParseTrustAnchors(Some(e)))?;
            trust_anchors.push(ta);
        }
        let trust_anchors = webpki::TLSServerTrustAnchors(&trust_anchors);

        let end_entity_cert = load_file_contents(&settings.end_entity_cert)?;

        // XXX: Assume there are no intermediates since there is no way to load
        // them yet.
        let cert_chain = vec![rustls::Certificate(end_entity_cert)];

        // Load the private key after we've validated the certificate.
        let private_key = load_file_contents(&settings.private_key)?;
        let private_key = untrusted::Input::from(&private_key);

        // `CertResolver::new` is responsible for the consistency check.
        let cert_resolver = CertResolver::new(&trust_anchors, cert_chain, private_key)?;

        Ok(Self {
            cert_resolver: Arc::new(cert_resolver),
        })
    }
}

impl ServerConfig {
    pub fn from(common: &CommonConfig) -> Self {
        let mut config = rustls::ServerConfig::new(Arc::new(rustls::NoClientAuth));
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
}
