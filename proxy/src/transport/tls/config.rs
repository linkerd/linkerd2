use std::{
    fs::{self, File},
    io::{self, Cursor, Read},
    path::PathBuf,
    sync::Arc,
    time::{Duration, Instant, SystemTime,},
};

use super::{
    cert_resolver::CertResolver,

    rustls,
    untrusted,
    webpki,
};

use futures::{future, Future, Sink, Stream};
use futures_watch::Watch;
use tokio::timer::Interval;

pub type ServerConfigWatch = Watch<Option<ServerConfig>>;

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

impl CommonSettings {

    fn change_timestamps(&self, interval: Duration) -> impl Stream<Item = (), Error = ()> {
        let paths = [
            self.trust_anchors.clone(),
            self.end_entity_cert.clone(),
            self.private_key.clone(),
        ];
        let mut max: Option<SystemTime> = None;
        Interval::new(Instant::now(), interval)
            .map_err(|e| error!("timer error: {:?}", e))
            .filter_map(move |_| {
                for path in &paths  {
                    let t = fs::metadata(path)
                        .and_then(|meta| meta.modified())
                        .map_err(|e| if e.kind() != io::ErrorKind::NotFound {
                            // Don't log if the files don't exist, since this
                            // makes the logs *quite* noisy.
                            warn!("metadata for {:?}: {}", path, e)
                        })
                        .ok();
                    if t > max {
                        max = t;
                        trace!("{:?} changed at {:?}", path, t);
                        return Some(());
                    }
                }
                None
            })
    }

    /// Stream changes to the files described by this `CommonSettings`.
    ///
    /// This will poll the filesystem for changes to the files at the paths
    /// described by this `CommonSettings` every `interval`, and attempt to
    /// load a new `CommonConfig` from the files again after each change.
    ///
    /// The returned stream consists of each subsequent successfully loaded
    /// `CommonSettings` after each change. If the settings could not be
    /// reloaded (i.e., they were malformed), nothing is sent.
    ///
    /// TODO: On Linux, this should be replaced with an `inotify` watch when
    ///       available.
    pub fn stream_changes(self, interval: Duration)
        -> impl Stream<Item = CommonConfig, Error = ()>
    {
        self.change_timestamps(interval)
            .filter_map(move |_|
                CommonConfig::load_from_disk(&self)
                    .map_err(|e| warn!("error reloading TLS config: {:?}, falling back", e))
                    .ok()
            )
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

    /// Watch a `Stream` of changes to a `CommonConfig`, such as those returned by
    /// `CommonSettings::stream_changes`, and update a `futures_watch::Watch` cell
    /// with a `ServerConfig` generated from each change.
    pub fn watch<C>(changes: C)
        -> (ServerConfigWatch, Box<Future<Item=(), Error=()> + Send>)
    where
        C: Stream<Item = CommonConfig, Error = ()> + Send + 'static,
    {
        let (watch, store) = Watch::new(None);
        let server_configs = changes.map(|ref config| Self::from(config));
        let store = store
            .sink_map_err(|_| warn!("all server config watches dropped"));
        let f = server_configs.map(Some).forward(store)
            .map(|_| trace!("forwarding to server config watch finished."));

        // NOTE: This function and `no_tls` return `Box<Future<...>>` rather
        //       than `impl Future<...>` so that they can have the _same_
        //       return types (impl Traits are not the same type unless the
        //       original non-anonymized type was the same).
        (watch, Box::new(f))
    }

    pub fn no_tls()
        -> (ServerConfigWatch, Box<Future<Item = (), Error = ()> + Send>)
    {
            let (watch, _) = Watch::new(None);
            let no_future = future::ok(());

            (watch, Box::new(no_future))
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
