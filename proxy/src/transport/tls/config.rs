use std::{
    fs::File,
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

use futures::{future, Future, Stream};
use futures_watch::Watch;
use tokio::timer::Interval;

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
}

/// Validated configuration common between TLS clients and TLS servers.
pub struct CommonConfig {
    cert_resolver: Arc<CertResolver>,
}

/// Validated configuration for TLS clients.
///
/// TODO: Fill this in with the actual configuration.
#[derive(Clone, Debug)]
pub struct ClientConfig(Arc<()>);

/// Validated configuration for TLS servers.
#[derive(Clone)]
pub struct ServerConfig(pub(super) Arc<rustls::ServerConfig>);

pub type ClientConfigWatch = Watch<Option<ClientConfig>>;
pub type ServerConfigWatch = Watch<Option<ServerConfig>>;

#[derive(Debug)]
pub enum Error {
    Io(PathBuf, io::Error),
    FailedToParseTrustAnchors(Option<webpki::Error>),
    EndEntityCertIsNotValid(webpki::Error),
    InvalidPrivateKey,
    TimeConversionFailed,
    #[cfg(target_os = "linux")]
    InotifyInit(io::Error),
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
        // If we're on Linux, first atttempt to start an Inotify watch on the
        // paths. If this fails, fall back to polling the filesystem.
        #[cfg(target_os = "linux")]
        let changes: Box<Stream<Item = (), Error = ()> + Send> =
            match self.stream_changes_inotify() {
                Ok(s) => Box::new(s),
                Err(e) => {
                    warn!(
                        "inotify init error: {:?}, falling back to polling",
                        e
                    );
                    Box::new(self.stream_changes_polling(interval))
                },
            };

        // If we're not on Linux, we can't use inotify, so simply poll the fs.
        // TODO: Use other FS events APIs (such as `kqueue`) as well, when
        //       they're available.
        #[cfg(not(target_os = "linux"))]
        let changes = self.stream_changes_polling(interval);

        changes.filter_map(move |_|
            CommonConfig::load_from_disk(&self)
                .map_err(|e| warn!("error reloading TLS config: {:?}, falling back", e))
                .ok()
        )

    }

    /// Stream changes by polling the filesystem.
    ///
    /// This will poll the filesystem for changes to the files at the paths
    /// described by this `CommonSettings` every `interval`, and attempt to
    /// load a new `CommonConfig` from the files again after each change.
    ///
    /// This is used on operating systems other than Linux, or on Linux if
    /// our attempt to use `inotify` failed.
    fn stream_changes_polling(&self, interval: Duration)
        -> impl Stream<Item = (), Error = ()>
    {
        fn last_modified(path: &PathBuf) -> Option<SystemTime> {
            // We have to canonicalize the path _every_ time we poll the fs,
            // rather than once when we start watching, because if it's a
            // symlink, the target may change. If that happened, and we
            // continued watching the original canonical path, we wouldn't see
            // any subsequent changes to the new symlink target.
            path.canonicalize()
                .and_then(|canonical| {
                    trace!("last_modified: {:?} -> {:?}", path, canonical);
                    canonical.symlink_metadata()
                        .and_then(|meta| meta.modified())
                })
                .map_err(|e| if e.kind() != io::ErrorKind::NotFound {
                    // Don't log if the files don't exist, since this
                    // makes the logs *quite* noisy.
                    warn!("error reading metadata for {:?}: {}", path, e)
                })
                .ok()
        }

        let paths = self.paths().iter()
            .map(|&p| p.clone())
            .collect::<Vec<PathBuf>>();

        let mut max: Option<SystemTime> = None;

        Interval::new(Instant::now(), interval)
            .map_err(|e| error!("timer error: {:?}", e))
            .filter_map(move |_| {
                for path in &paths  {
                    let t = last_modified(path);
                    if t > max {
                        max = t;
                        trace!("{:?} changed at {:?}", path, t);
                        return Some(());
                    }
                }
                None
            })
    }

    #[cfg(target_os = "linux")]
    fn stream_changes_inotify(&self)
        -> Result<impl Stream<Item = (), Error = ()>, Error>
    {
        use std::{collections::HashSet, path::Path};
        use inotify::{Inotify, WatchMask};

        // Use a broad watch mask so that we will pick up any events that might
        // indicate a change to the watched files.
        //
        // Such a broad mask may lead to reloading certs multiple times when k8s
        // modifies a ConfigMap or Secret, which is a multi-step process that we
        // see as a series CREATE, MOVED_TO, MOVED_FROM, and DELETE events.
        // However, we want to catch single events that might occur when the
        // files we're watching *don't* live in a k8s ConfigMap/Secret.
        let mask = WatchMask::CREATE
                 | WatchMask::MODIFY
                 | WatchMask::DELETE
                 | WatchMask::MOVE
                 ;
        let mut inotify = Inotify::init().map_err(Error::InotifyInit)?;

        let paths = self.paths();
        let paths = paths.into_iter()
            .map(|path| {
                // If the path to watch has a parent, watch that instead. This
                // will allow us to pick up events to files in k8s ConfigMaps
                // or Secrets (which we wouldn't detect if we watch the file
                // itself, as they are double-symlinked).
                //
                // This may also result in some false positives (if a file we
                // *don't* care about in the same dir changes, we'll still
                // reload), but that's unlikely to be a problem.
                let parent = path
                    .parent()
                    .map(Path::to_path_buf)
                    .unwrap_or(path.to_path_buf());
                trace!("will watch {:?} for {:?}", parent, path);
                path
            })
            // Collect the paths into a `HashSet` eliminates any duplicates, to
            // conserve the number of inotify watches we create.
            .collect::<HashSet<_>>();

        for path in paths {
            inotify.add_watch(path, mask)
                .map_err(|e| Error::Io(path.to_path_buf(), e))?;
            trace!("inotify: watch {:?}", path);
        }

        let events = inotify.into_event_stream()
            .map(|ev| {
                trace!("inotify: event={:?}; path={:?};", ev.mask, ev.name);
            })
            .map_err(|e| error!("inotify watch error: {}", e));
        trace!("started inotify watch");

        Ok(events)
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

pub fn watch_for_config_changes(settings: Option<&CommonSettings>)
    -> (ClientConfigWatch, ServerConfigWatch, Box<Future<Item = (), Error = ()> + Send>)
{
    let settings = if let Some(settings) = settings {
        settings.clone()
    } else {
        let (client_watch, _) = Watch::new(None);
        let (server_watch, _) = Watch::new(None);
        let no_future = future::ok(());
        return (client_watch, server_watch, Box::new(no_future));
    };

    let changes = settings.stream_changes(Duration::from_secs(1));
    let (client_watch, client_store) = Watch::new(None);
    let (server_watch, server_store) = Watch::new(None);

    // `Store::store` will return an error iff all watchers have been dropped,
    // so we'll use `fold` to cancel the forwarding future. Eventually, we can
    // also use the fold to continue tracking previous states if we need to do
    // that.
    let f = changes
        .fold(
            (client_store, server_store),
            |(mut client_store, mut server_store), ref config| {
                client_store
                    .store(Some(ClientConfig(Arc::new(()))))
                    .map_err(|_| trace!("all client config watchers dropped"))?;
                server_store
                    .store(Some(ServerConfig::from(config)))
                    .map_err(|_| trace!("all server config watchers dropped"))?;
                Ok((client_store, server_store))
            })
        .then(|_| {
            trace!("forwarding to server config watch finished.");
            Ok(())
        });

    // This function and `ServerConfig::no_tls` return `Box<Future<...>>`
    // rather than `impl Future<...>` so that they can have the _same_ return
    // types (impl Traits are not the same type unless the original
    // non-anonymized type was the same).
    (client_watch, server_watch, Box::new(f))
}

impl ServerConfig {
    fn from(common: &CommonConfig) -> Self {
        let mut config = rustls::ServerConfig::new(Arc::new(rustls::NoClientAuth));
        set_common_settings(&mut config.versions);
        config.cert_resolver = common.cert_resolver.clone();
        ServerConfig(Arc::new(config))
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
