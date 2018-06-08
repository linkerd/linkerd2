use std::{
    cell::RefCell,
    fs::{self, File},
    io::{self, Cursor, Read},
    path::PathBuf,
    sync::Arc,
    time::{Duration, Instant},
};

use super::{
    cert_resolver::CertResolver,

    ring::digest::{self, Digest},
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

#[derive(Clone, Debug)]
struct PathAndHash {
    /// The path to the file.
    path: PathBuf,

    /// The last SHA-384 digest of the file, if we have previously hashed it.
    last_hash: RefCell<Option<Digest>>,
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
    /// This will calculate the SHA-384 hash of each of files at the paths
    /// described by this `CommonSettings` every `interval`, and attempt to
    /// load a new `CommonConfig` from the files again if any of the hashes
    /// has changed.
    ///
    /// This is used on operating systems other than Linux, or on Linux if
    /// our attempt to use `inotify` failed.
    fn stream_changes_polling(&self, interval: Duration)
        -> impl Stream<Item = (), Error = ()>
    {
        let files = self.paths().iter()
            .map(|&p| PathAndHash::new(p.clone()))
            .collect::<Vec<_>>();

        Interval::new(Instant::now(), interval)
            .map_err(|e| error!("timer error: {:?}", e))
            .filter_map(move |_| {
                for file in &files  {
                    match file.has_changed() {
                        Ok(true) => {
                            trace!("{:?} changed", &file.path);
                            return Some(());
                        },
                        Err(ref e) if e.kind() != io::ErrorKind::NotFound => {
                            // Ignore file not found errors so the log doesn't
                            // get too noisy.
                            warn!("error hashing {:?}: {}", &file.path, e);
                        },
                        _ => {
                            // If the file doesn't exist or the hash hasn't changed,
                            // keep going.
                        },
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

impl PathAndHash {
    fn new(path: PathBuf) -> Self {
        Self {
            path,
            last_hash: RefCell::new(None),
        }
    }

    fn has_changed(&self) -> io::Result<bool> {
        let contents = fs::read(&self.path)?;
        let hash = Some(digest::digest(&digest::SHA256, &contents[..]));
        let changed = self.last_hash
            .borrow().as_ref()
            .map(Digest::as_ref) != hash.as_ref().map(Digest::as_ref);
        if changed {
            self.last_hash.replace(hash);
            Ok(true)
        } else {
            Ok(false)
        }

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

#[cfg(test)]
mod tests {
    use super::*;

    use tempdir::TempDir;
    use tokio::{
        runtime::current_thread::Runtime,
        timer,
    };

    use std::{
        path::Path,
        io::{self, Write},
        fs::{self, File},
        thread,
    };

    use futures::{Sink, Stream};
    use futures_watch::Watch;

    // These functions are a workaround for Windows having separate API calls
    // for symlinking files and directories. You're welcome, Brian ;)
    fn symlink_file<P: AsRef<Path>, Q: AsRef<Path>>(src: P, dst: Q) -> io::Result<()> {
        #[cfg(target_os = "windows")] {
            ::std::os::windows::fs::symlink_file(src, dst)?;
        }
        #[cfg(not(target_os = "windows"))] {
            ::std::os::unix::fs::symlink(src, dst)?;
        }
        Ok(())
    }

    fn symlink_dir<P: AsRef<Path>, Q: AsRef<Path>>(src: P, dst: Q) -> io::Result<()> {
        #[cfg(target_os = "windows")] {
            ::std::os::windows::fs::symlink_dir(src, dst)?;
        }
        #[cfg(not(target_os = "windows"))] {
            ::std::os::unix::fs::symlink(src, dst)?;
        }
        Ok(())
    }

    struct Fixture {
        cfg: CommonSettings,
        dir: TempDir,
        rt: Runtime,
    }

    const END_ENTITY_CERT: &'static str = "test-test.crt";
    const PRIVATE_KEY: &'static str = "test-test.p8";
    const TRUST_ANCHORS: &'static str = "ca.pem";

    /// A trait that allows an executor to execute a future for up to
    /// a given time limit, and then panics if the future has not
    /// finished.
    ///
    // TODO: This might be useful for tests outside of this module...
    trait BlockOnFor {
        /// Runs the provided future for up to `Duration`, blocking the thread
        /// until the future completes.
        fn block_on_for<F>(&mut self, duration: Duration, f: F) -> Result<F::Item, F::Error>
        where
            F: Future;
    }

    impl BlockOnFor for Runtime {
        fn block_on_for<F>(&mut self, duration: Duration, f: F) -> Result<F::Item, F::Error>
        where
            F: Future
        {
            let f = timer::Deadline::new(f, Instant::now() + duration);
            match self.block_on(f) {
                Ok(item) => Ok(item),
                Err(e) => if e.is_inner() {
                    return Err(e.into_inner().unwrap());
                } else if e.is_timer() {
                    panic!("timer error: {}", e.into_timer().unwrap());
                } else {
                    panic!("assertion failed: future did not finish within {:?}", duration);
                },
            }
        }
    }

    fn fixture() -> Fixture {
        let dir = TempDir::new("certs").expect("temp dir");
        let cfg = CommonSettings {
            trust_anchors: dir.path().join(TRUST_ANCHORS),
            end_entity_cert: dir.path().join(END_ENTITY_CERT),
            private_key: dir.path().join(PRIVATE_KEY),
        };
        let rt = Runtime::new().expect("runtime");
        Fixture { cfg, dir, rt }
    }

    fn watch_stream(stream: impl Stream<Item = (), Error = ()> + 'static)
        -> (Watch<()>, Box<Future<Item = (), Error = ()>>)
    {
        let (watch, store) = Watch::new(());
        // Use a watch so we can start running the stream immediately but also
        // wait on stream updates.
        let f = stream
            .forward(store.sink_map_err(|_| ()))
            .map(|_| ())
            .map_err(|_| ());

        (watch, Box::new(f))
    }

    fn test_detects_create(
        fixture: Fixture,
        stream: impl Stream<Item = (), Error=()> + 'static,
    ) {
        let Fixture { cfg, dir: _dir, mut rt } = fixture;

        let (watch, bg) = watch_stream(stream);
        rt.spawn(bg);

        let f = File::create(cfg.trust_anchors)
            .expect("create trust anchors");
        f.sync_all().expect("create trust anchors");
        println!("created {:?}", f);

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("first change");
        assert!(item.is_some());

        let f = File::create(cfg.end_entity_cert)
            .expect("create end entity cert");
        f.sync_all()
            .expect("sync end entity cert");
        println!("created {:?}", f);

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("second change");
        assert!(item.is_some());

        let f = File::create(cfg.private_key)
            .expect("create private key");
        f.sync_all()
            .expect("sync private key");
        println!("created {:?}", f);

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, _) = rt.block_on_for(Duration::from_secs(2), next)
            .expect("third change");
        assert!(item.is_some());
    }

    fn test_detects_create_symlink(
        fixture: Fixture,
        stream: impl Stream<Item = (), Error=()> + 'static,
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let data_path = dir.path().join("data");
        fs::create_dir(&data_path).expect("create data dir");

        let trust_anchors_path = data_path.clone().join(TRUST_ANCHORS);
        let end_entity_cert_path = data_path.clone().join(END_ENTITY_CERT);
        let private_key_path = data_path.clone().join(PRIVATE_KEY);

        let end_entity_cert = File::create(&end_entity_cert_path)
            .expect("create end entity cert");
        end_entity_cert.sync_all()
            .expect("sync end entity cert");
        let private_key = File::create(&private_key_path)
            .expect("create private key");
        private_key.sync_all()
            .expect("sync private key");
        let trust_anchors = File::create(&trust_anchors_path)
            .expect("create trust anchors");
        trust_anchors.sync_all()
            .expect("sync trust_anchors");

        let (watch, bg) = watch_stream(stream);
        rt.spawn(bg);

        symlink_file(trust_anchors_path, cfg.trust_anchors)
            .expect("symlink trust anchors");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("first change");
        assert!(item.is_some());

        symlink_file(private_key_path, cfg.private_key)
            .expect("symlink private key");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("second change");
        assert!(item.is_some());

        symlink_file(end_entity_cert_path, cfg.end_entity_cert)
            .expect("symlink end entity cert");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, _) = rt.block_on_for(Duration::from_secs(2), next)
            .expect("third change");
        assert!(item.is_some());
    }

    // Test for when the watched files are symlinks to a file insdie of a
    // directory which is also a symlink (as is the case with Kubernetes
    // ConfigMap/Secret volume mounts).
    fn test_detects_create_double_symlink(
        fixture: Fixture,
        stream: impl Stream<Item = (), Error=()> + 'static,
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let real_data_path = dir.path().join("real_data");
        let data_path = dir.path().join("data");
        fs::create_dir(&real_data_path).expect("create data dir");
        symlink_dir(&real_data_path, &data_path).expect("create data dir symlink");

        let end_entity_cert = File::create(real_data_path.clone().join(END_ENTITY_CERT))
            .expect("create end entity cert");
        end_entity_cert.sync_all()
            .expect("sync end entity cert");
        let private_key = File::create(real_data_path.clone().join(PRIVATE_KEY))
            .expect("create private key");
        private_key.sync_all()
            .expect("sync private key");
        let trust_anchors = File::create(real_data_path.clone().join(TRUST_ANCHORS))
            .expect("create trust anchors");
        trust_anchors.sync_all()
            .expect("sync private key");

        let (watch, bg) = watch_stream(stream);
        rt.spawn(bg);

        symlink_file(data_path.clone().join(TRUST_ANCHORS), cfg.trust_anchors)
            .expect("symlink trust anchors");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("first change");
        assert!(item.is_some());

        // Sleep briefly between changing the fs, as a workaround for
        // macOS reporting timestamps with second resolution.
        // https://github.com/runconduit/conduit/issues/1090
        thread::sleep(Duration::from_secs(2));
        symlink_file(data_path.clone().join(PRIVATE_KEY), cfg.private_key)
            .expect("symlink private key");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("second change");
        assert!(item.is_some());

        // Sleep briefly between changing the fs, as a workaround for
        // macOS reporting timestamps with second resolution.
        // https://github.com/runconduit/conduit/issues/1090
        thread::sleep(Duration::from_secs(2));
        symlink_file(real_data_path.clone().join(END_ENTITY_CERT), cfg.end_entity_cert)
            .expect("symlink end entity cert");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, _) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("third change");
        assert!(item.is_some());
    }

    fn test_detects_modification_symlink(
        fixture: Fixture,
        stream: impl Stream<Item = (), Error=()> + 'static,
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let data_path = dir.path().join("data");
        fs::create_dir(&data_path).expect("create data dir");

        let trust_anchors_path = data_path.clone().join(TRUST_ANCHORS);
        let end_entity_cert_path = data_path.clone().join(END_ENTITY_CERT);
        let private_key_path = data_path.clone().join(PRIVATE_KEY);

        let mut trust_anchors = File::create(&trust_anchors_path)
            .expect("create trust anchors");
        println!("created {:?}", trust_anchors);
        trust_anchors.write_all(b"I am not real trust anchors")
            .expect("write to trust anchors");
        trust_anchors.sync_all().expect("sync trust anchors");

        let mut private_key = File::create(&private_key_path)
            .expect("create private key");
        println!("created {:?}", private_key);
        private_key.write_all(b"I am not a realprivate key")
            .expect("write to private key");
        private_key.sync_all().expect("sync private key");

        let mut end_entity_cert = File::create(&end_entity_cert_path)
            .expect("create end entity cert");
        println!("created {:?}", end_entity_cert);
        end_entity_cert.write_all(b"I am not real end entity cert")
            .expect("write to end entity cert");
        end_entity_cert.sync_all().expect("sync end entity cert");

        symlink_file(private_key_path, cfg.private_key)
            .expect("symlink private key");
        symlink_file(end_entity_cert_path, cfg.end_entity_cert)
            .expect("symlink end entity cert");
        symlink_file(trust_anchors_path, cfg.trust_anchors)
            .expect("symlink trust anchors");

        let (watch, bg) = watch_stream(stream);
        rt.spawn(Box::new(bg));

        trust_anchors.write_all(b"Trust me on this :)")
            .expect("write to trust anchors again");
        trust_anchors.sync_all()
            .expect("sync trust anchors again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("first change");
        assert!(item.is_some());
        println!("saw first change");

        end_entity_cert.write_all(b"This is the end of the end entity cert :)")
            .expect("write to end entity cert again");
        end_entity_cert.sync_all()
            .expect("sync end entity cert again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("second change");
        assert!(item.is_some());
        println!("saw second change");

        private_key.write_all(b"Keep me private :)")
            .expect("write to private key");
        private_key.sync_all()
            .expect("sync private key again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, _) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("third change");
        assert!(item.is_some());
        println!("saw third change");
    }

    fn test_detects_modification(
        fixture: Fixture,
        stream: impl Stream<Item = (), Error=()> + 'static,
    ) {
        let Fixture { cfg, dir: _dir, mut rt } = fixture;

        let mut trust_anchors = File::create(cfg.trust_anchors.clone())
            .expect("create trust anchors");
        println!("created {:?}", trust_anchors);
        trust_anchors.write_all(b"I am not real trust anchors")
            .expect("write to trust anchors");
        trust_anchors.sync_all().expect("sync trust anchors");

        let mut private_key = File::create(cfg.private_key.clone())
            .expect("create private key");
        println!("created {:?}", private_key);
        private_key.write_all(b"I am not a realprivate key")
            .expect("write to private key");
        private_key.sync_all().expect("sync private key");

        let mut end_entity_cert = File::create(cfg.end_entity_cert.clone())
            .expect("create end entity cert");
        println!("created {:?}", end_entity_cert);
        end_entity_cert.write_all(b"I am not real end entity cert")
            .expect("write to end entity cert");
        end_entity_cert.sync_all().expect("sync end entity cert");

        let (watch, bg) = watch_stream(stream);
        rt.spawn(Box::new(bg));

        trust_anchors.write_all(b"Trust me on this :)")
            .expect("write to trust anchors again");
        trust_anchors.sync_all()
            .expect("sync trust anchors again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("first change");
        assert!(item.is_some());
        println!("saw first change");

        end_entity_cert.write_all(b"This is the end of the end entity cert :)")
            .expect("write to end entity cert again");
        end_entity_cert.sync_all()
            .expect("sync end entity cert again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("second change");
        assert!(item.is_some());
        println!("saw second change");

        private_key.write_all(b"Keep me private :)")
            .expect("write to private key");
        private_key.sync_all()
            .expect("sync private key again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, _) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("third change");
        assert!(item.is_some());
        println!("saw third change");
    }

    fn test_detects_modification_double_symlink(
        fixture: Fixture,
        stream: impl Stream<Item = (), Error=()> + 'static,
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let real_data_path = dir.path().join("real_data");
        let data_path = dir.path().join("data");
        fs::create_dir(&real_data_path).expect("create data dir");
        symlink_dir(&real_data_path, &data_path).expect("create data dir symlink");

        let mut trust_anchors = File::create(real_data_path.clone().join(TRUST_ANCHORS))
            .expect("create trust anchors");
        println!("created {:?}", trust_anchors);
        trust_anchors.write_all(b"I am not real trust anchors")
            .expect("write to trust anchors");
        trust_anchors.sync_all().expect("sync trust anchors");

        let mut private_key = File::create(real_data_path.clone().join(PRIVATE_KEY))
            .expect("create private key");
        println!("created {:?}", private_key);
        private_key.write_all(b"I am not a realprivate key")
            .expect("write to private key");
        private_key.sync_all().expect("sync private key");

        let mut end_entity_cert = File::create(real_data_path.clone().join(END_ENTITY_CERT))
            .expect("create end entity cert");
        println!("created {:?}", end_entity_cert);
        end_entity_cert.write_all(b"I am not real end entity cert")
            .expect("write to end entity cert");
        end_entity_cert.sync_all().expect("sync end entity cert");

        symlink_file(data_path.clone().join(PRIVATE_KEY), cfg.private_key)
            .expect("symlink private key");
        symlink_file(data_path.clone().join(END_ENTITY_CERT), cfg.end_entity_cert)
            .expect("symlink end entity cert");
        symlink_file(data_path.clone().join(TRUST_ANCHORS), cfg.trust_anchors)
            .expect("symlink trust anchors");

        let (watch, bg) = watch_stream(stream);
        rt.spawn(Box::new(bg));

        trust_anchors.write_all(b"Trust me on this :)")
            .expect("write to trust anchors again");
        trust_anchors.sync_all()
            .expect("sync trust anchors again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("first change");
        assert!(item.is_some());
        println!("saw first change");

        end_entity_cert.write_all(b"This is the end of the end entity cert :)")
            .expect("write to end entity cert again");
        end_entity_cert.sync_all()
            .expect("sync end entity cert again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, watch) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("second change");
        assert!(item.is_some());
        println!("saw second change");

        private_key.write_all(b"Keep me private :)")
            .expect("write to private key");
        private_key.sync_all()
            .expect("sync private key again");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, _) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("third change");
        assert!(item.is_some());
        println!("saw third change");
    }

    fn test_detects_double_symlink_retargeting(
        fixture: Fixture,
        stream: impl Stream<Item = (), Error=()> + 'static,
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let real_data_path = dir.path().join("real_data");
        let real_data_path_2 = dir.path().join("real_data_2");
        let data_path = dir.path().join("data");
        fs::create_dir(&real_data_path).expect("create data dir");
        fs::create_dir(&real_data_path_2).expect("create data dir 2");
        symlink_dir(&real_data_path, &data_path).expect("create data dir symlink");

        let mut trust_anchors = File::create(real_data_path.clone().join(TRUST_ANCHORS))
            .expect("create trust anchors");
        println!("created {:?}", trust_anchors);
        trust_anchors.write_all(b"I am not real trust anchors")
            .expect("write to trust anchors");
        trust_anchors.sync_all().expect("sync trust anchors");

        let mut private_key = File::create(real_data_path.clone().join(PRIVATE_KEY))
            .expect("create private key");
        println!("created {:?}", private_key);
        private_key.write_all(b"I am not a realprivate key")
            .expect("write to private key");
        private_key.sync_all().expect("sync private key");

        let mut end_entity_cert = File::create(real_data_path.clone().join(END_ENTITY_CERT))
            .expect("create end entity cert");
        println!("created {:?}", end_entity_cert);
        end_entity_cert.write_all(b"I am not real end entity cert")
            .expect("write to end entity cert");
        end_entity_cert.sync_all().expect("sync end entity cert");

        let mut trust_anchors = File::create(real_data_path_2.clone().join(TRUST_ANCHORS))
            .expect("create trust anchors 2");
        println!("created {:?}", trust_anchors);
        trust_anchors.write_all(b"I am not real trust anchors either")
            .expect("write to trust anchors 2");
        trust_anchors.sync_all().expect("sync trust anchors 2");

        let mut private_key = File::create(real_data_path_2.clone().join(PRIVATE_KEY))
            .expect("create private key 2");
        println!("created {:?}", private_key);
        private_key.write_all(b"I am not a real private key either")
            .expect("write to private key 2");
        private_key.sync_all().expect("sync private key 2");

        let mut end_entity_cert = File::create(real_data_path_2.clone().join(END_ENTITY_CERT))
            .expect("create end entity cert 2");
        println!("created {:?}", end_entity_cert);
        end_entity_cert.write_all(b"I am not real end entity cert either")
            .expect("write to end entity cert 2");
        end_entity_cert.sync_all().expect("sync end entity cert 2");

        symlink_file(data_path.clone().join(PRIVATE_KEY), cfg.private_key)
            .expect("symlink private key");
        symlink_file(data_path.clone().join(END_ENTITY_CERT), cfg.end_entity_cert)
            .expect("symlink end entity cert");
        symlink_file(data_path.clone().join(TRUST_ANCHORS), cfg.trust_anchors)
            .expect("symlink trust anchors");

        let (watch, bg) = watch_stream(stream);
        rt.spawn(Box::new(bg));

        fs::remove_dir_all(&data_path)
            .expect("remove original data dir symlink");
        symlink_dir(&real_data_path_2, &data_path)
            .expect("create second data dir symlink");

        let next = watch.into_future().map_err(|(e, _)| e);
        let (item, _) = rt.block_on_for(Duration::from_secs(5), next)
            .expect("first change");
        assert!(item.is_some());
        println!("saw first change");
    }


    #[test]
    fn polling_detects_create() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_polling(Duration::from_millis(1));
        test_detects_create(fixture, stream)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_create() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_inotify();
        test_detects_create(fixture, stream)
    }

    #[test]
    fn polling_detects_create_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_polling(Duration::from_millis(1));
        test_detects_create_symlink(fixture, stream)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_create_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_inotify();
        test_detects_create_symlink(fixture, stream)
    }

    #[test]
    fn polling_detects_create_double_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_polling(Duration::from_millis(1));
        test_detects_create_double_symlink(fixture, stream)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_create_double_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_inotify();
        test_detects_create_double_symlink(fixture, stream)
    }

    #[test]
    fn polling_detects_modification() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_polling(Duration::from_millis(1));
        test_detects_modification(fixture, stream)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_modification() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_inotify();
        test_detects_modification(fixture, stream)
    }

    #[test]
    fn polling_detects_modification_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_polling(Duration::from_millis(1));
        test_detects_modification_symlink(fixture, stream)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_modification_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_inotify();
        test_detects_modification_symlink(fixture, stream)
    }

    #[test]
    fn polling_detects_modification_double_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_polling(Duration::from_millis(1));
        test_detects_modification_double_symlink(fixture, stream)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_modification_double_symlink() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_inotify();
        test_detects_modification_double_symlink(fixture, stream)
    }

    #[test]
    fn polling_detects_double_symlink_retargeting() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_polling(Duration::from_millis(1));
        test_detects_double_symlink_retargeting(fixture, stream)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_double_symlink_retargeting() {
        let fixture = fixture();
        let stream = fixture.cfg.clone()
            .stream_changes_inotify();
        test_detects_double_symlink_retargeting(fixture, stream)
    }

}
