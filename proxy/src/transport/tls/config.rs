use std::{
    fs::File,
    io::{self, Cursor, Read},
    path::PathBuf,
    sync::Arc,
    time::Duration,
};

use super::{
    cert_resolver::CertResolver,

    rustls,
    untrusted,
    webpki,
};

use futures::{future, Future, Stream};
use futures_watch::Watch;

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
    pub fn stream_changes(self, interval: Duration)
        -> impl Stream<Item = CommonConfig, Error = ()>
    {
        let paths = self.paths().iter()
            .map(|&p| p.clone())
            .collect::<Vec<_>>();
        ::fs_watch::stream_changes(paths, interval)
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
    use ::fs_watch;
    use task::test_util::BlockOnFor;

    use tempdir::TempDir;
    use tokio::runtime::current_thread::Runtime;

    use std::{
        path::Path,
        io::Write,
        fs::{self, File},
    };
    #[cfg(not(target_os = "windows"))]
    use std::os::unix::fs::symlink;

    use futures::{Sink, Stream};
    use futures_watch::{Watch, WatchError};

    struct Fixture {
        cfg: CommonSettings,
        dir: TempDir,
        rt: Runtime,
    }

    const END_ENTITY_CERT: &'static str = "test-test.crt";
    const PRIVATE_KEY: &'static str = "test-test.p8";
    const TRUST_ANCHORS: &'static str = "ca.pem";

    impl Fixture {
        fn new() -> Fixture {
            let _ = ::env_logger::try_init();
            let dir = TempDir::new("certs").unwrap();
            let cfg = CommonSettings {
                trust_anchors: dir.path().join(TRUST_ANCHORS),
                end_entity_cert: dir.path().join(END_ENTITY_CERT),
                private_key: dir.path().join(PRIVATE_KEY),
            };
            let rt = Runtime::new().unwrap();
            Fixture { cfg, dir, rt }
        }

        fn test_polling(
            self,
            test: fn(Self, Watch<()>, Box<Future<Item=(), Error = ()>>)
        ) {
            let paths = self.cfg.paths().iter()
                .map(|&p| p.clone())
                .collect::<Vec<PathBuf>>();
            let stream = fs_watch::stream_changes_polling(
                paths,
                Duration::from_secs(1)
            );
            let (watch, bg) = watch_stream(stream);
            test(self, watch, bg)
        }

        #[cfg(target_os="linux")]
        fn test_inotify(
            self,
            test: fn(Self, Watch<()>, Box<Future<Item=(), Error = ()>>)
        ) {
            let paths = self.cfg.paths().iter()
                .map(|&p| p.clone())
                .collect::<Vec<PathBuf>>();
            let stream = fs_watch::inotify::WatchStream::new(paths)
                .unwrap()
                .map_err(|e| panic!("{}", e));
            let (watch, bg) = watch_stream(stream);
            test(self, watch, bg)
        }
    }

    fn create_file<P: AsRef<Path>>(path: P) -> io::Result<File> {
        let f = File::create(path)?;
        f.sync_all()?;
        println!("created {:?}", f);
        Ok(f)
    }

    fn create_and_write<P: AsRef<Path>>(path: P, s: &[u8]) -> io::Result<File> {
        let mut f = File::create(path)?;
        f.write_all(s)?;
        f.sync_all()?;
        println!("created and wrote to {:?}", f);
        Ok(f)
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

    fn next_change(rt: &mut Runtime, watch: Watch<()>)
        -> Result<(Option<()>, Watch<()>), WatchError>
    {
        let next = watch.into_future().map_err(|(e, _)| e);
        rt.block_on_for(Duration::from_secs(2), next)
    }

    fn test_detects_create(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir: _dir, mut rt } = fixture;

        rt.spawn(bg);

        create_file(cfg.trust_anchors).unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        create_file(cfg.end_entity_cert).unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        create_file(cfg.private_key).unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
    }

    fn test_detects_delete_and_recreate(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir: _dir, mut rt } = fixture;
        rt.spawn(bg);

        create_file(cfg.trust_anchors).unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        create_file(cfg.end_entity_cert).unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        create_and_write(&cfg.private_key, b"i'm the first private key").unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        fs::remove_file(&cfg.private_key).unwrap();
        println!("deleted private key");

        create_and_write(&cfg.private_key, b"i'm the new private key").unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
    }

    #[cfg(not(target_os = "windows"))]
    fn test_detects_create_symlink(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let data_path = dir.path().join("data");
        fs::create_dir(&data_path).unwrap();

        let trust_anchors_path = data_path.clone().join(TRUST_ANCHORS);
        let end_entity_cert_path = data_path.clone().join(END_ENTITY_CERT);
        let private_key_path = data_path.clone().join(PRIVATE_KEY);

        create_file(&end_entity_cert_path).unwrap();
        create_file(&private_key_path).unwrap();
        create_file(&trust_anchors_path).unwrap();

        rt.spawn(bg);

        symlink(trust_anchors_path, cfg.trust_anchors).unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        symlink(private_key_path, cfg.private_key).unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        symlink(end_entity_cert_path, cfg.end_entity_cert).unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
    }

    // Test for when the watched files are symlinks to a file insdie of a
    // directory which is also a symlink (as is the case with Kubernetes
    // ConfigMap/Secret volume mounts).
    #[cfg(not(target_os = "windows"))]
    fn test_detects_create_double_symlink(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let real_data_path = dir.path().join("real_data");
        let data_path = dir.path().join("data");
        fs::create_dir(&real_data_path).unwrap();
        symlink(&real_data_path, &data_path).unwrap();

        create_file(real_data_path.clone().join(END_ENTITY_CERT)).unwrap();
        create_file(real_data_path.clone().join(PRIVATE_KEY)).unwrap();
        create_file(real_data_path.clone().join(TRUST_ANCHORS)).unwrap();

        rt.spawn(bg);

        symlink(data_path.clone().join(TRUST_ANCHORS), cfg.trust_anchors)
            .unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        symlink(data_path.clone().join(PRIVATE_KEY), cfg.private_key)
            .unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());

        symlink(real_data_path.clone().join(END_ENTITY_CERT), cfg.end_entity_cert)
            .unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
    }

    #[cfg(not(target_os = "windows"))]
    fn test_detects_modification_symlink(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let data_path = dir.path().join("data");
        fs::create_dir(&data_path).unwrap();

        let trust_anchors_path = data_path.clone().join(TRUST_ANCHORS);
        let end_entity_cert_path = data_path.clone().join(END_ENTITY_CERT);
        let private_key_path = data_path.clone().join(PRIVATE_KEY);

        let mut trust_anchors = create_and_write(
            &trust_anchors_path,
            b"I am not real trust anchors",
        ).unwrap();

        let mut private_key = create_and_write(
            &private_key_path,
            b"I am not a realprivate key",
        ).unwrap();

        let mut end_entity_cert =  create_and_write(
            &end_entity_cert_path,
            b"I am not real end entity cert",
        ).unwrap();

        symlink(private_key_path, cfg.private_key).unwrap();
        symlink(end_entity_cert_path, cfg.end_entity_cert).unwrap();
        symlink(trust_anchors_path, cfg.trust_anchors).unwrap();

        rt.spawn(bg);

        trust_anchors.write_all(b"Trust me on this :)").unwrap();
        trust_anchors.sync_all().unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw first change");

        end_entity_cert.write_all(b"This is the end of the end entity cert :)")
            .unwrap();
        end_entity_cert.sync_all().unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw second change");

        private_key.write_all(b"Keep me private :)")
            .unwrap();
        private_key.sync_all()
            .unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw third change");
    }

    fn test_detects_modification(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir: _dir, mut rt } = fixture;

        let mut trust_anchors = create_and_write(
            &cfg.trust_anchors,
            b"I am not real trust anchors",
        ).unwrap();

        let mut private_key = create_and_write(
            &cfg.private_key,
            b"I am not a real private key",
        ).unwrap();

        let mut end_entity_cert = create_and_write(
            &cfg.end_entity_cert.clone(),
            b"I am not real end entity cert",
        ).unwrap();

        trust_anchors.write_all(b"Trust me on this :)").unwrap();
        trust_anchors.sync_all().unwrap();

        rt.spawn(bg);

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw first change");

        end_entity_cert.write_all(b"This is the end of the end entity cert :)")
            .unwrap();
        end_entity_cert.sync_all().unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw second change");

        private_key.write_all(b"Keep me private :)").unwrap();
        private_key.sync_all() .unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw third change");
    }

    #[cfg(not(target_os = "windows"))]
    fn test_detects_modification_double_symlink(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let real_data_path = dir.path().join("real_data");
        let data_path = dir.path().join("data");
        fs::create_dir(&real_data_path).unwrap();
        symlink(&real_data_path, &data_path).unwrap();

        let mut trust_anchors = create_and_write(
            real_data_path.clone().join(TRUST_ANCHORS),
            b"I am not real trust anchors",
        ).unwrap();

        let mut private_key = create_and_write(
            real_data_path.clone().join(PRIVATE_KEY),
            b"I am not a real private key",
        ).unwrap();

        let mut end_entity_cert = create_and_write(
            real_data_path.clone().join(END_ENTITY_CERT),
            b"I am not real end entity cert"
        ).unwrap();

        symlink(data_path.clone().join(PRIVATE_KEY), cfg.private_key).unwrap();
        symlink(data_path.clone().join(END_ENTITY_CERT), cfg.end_entity_cert).unwrap();
        symlink(data_path.clone().join(TRUST_ANCHORS), cfg.trust_anchors).unwrap();

        rt.spawn(bg);

        trust_anchors.write_all(b"Trust me on this :)").unwrap();
        trust_anchors.sync_all().unwrap();


        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw first change");

        end_entity_cert.write_all(b"This is the end of the end entity cert :)")
            .unwrap();
        end_entity_cert.sync_all().unwrap();

        let (item, watch) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw second change");

        private_key.write_all(b"Keep me private :)").unwrap();
        private_key.sync_all()
            .unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw third change");
    }

    #[cfg(not(target_os = "windows"))]
    fn test_detects_double_symlink_retargeting(
        fixture: Fixture,
        watch: Watch<()>,
        bg: Box<Future<Item = (), Error = ()>>
    ) {
        let Fixture { cfg, dir, mut rt } = fixture;

        let real_data_path = dir.path().join("real_data");
        let real_data_path_2 = dir.path().join("real_data_2");
        let data_path = dir.path().join("data");
        fs::create_dir(&real_data_path).unwrap();
        fs::create_dir(&real_data_path_2).unwrap();
        symlink(&real_data_path, &data_path).unwrap();

        // Create the first set of files
        create_and_write(
            real_data_path.clone().join(TRUST_ANCHORS),
            b"I am not real trust anchors",
        ).unwrap();

        create_and_write(
            real_data_path.clone().join(PRIVATE_KEY),
            b"I am not a real private key",
        ).unwrap();

        create_and_write(
            real_data_path.clone().join(END_ENTITY_CERT),
            b"I am not real end entity cert"
        ).unwrap();

        // Symlink those files into `data_path`
        symlink(data_path.clone().join(PRIVATE_KEY), cfg.private_key).unwrap();
        symlink(data_path.clone().join(END_ENTITY_CERT), cfg.end_entity_cert).unwrap();
        symlink(data_path.clone().join(TRUST_ANCHORS), cfg.trust_anchors) .unwrap();

        // Create the second set of files.
        create_and_write(
            real_data_path_2.clone().join(TRUST_ANCHORS),
            b"I am not real trust anchors either",
        ).unwrap();

        create_and_write(
            real_data_path_2.clone().join(PRIVATE_KEY),
            b"I am not real end entity cert either",
        ).unwrap();

        create_and_write(
            real_data_path_2.clone().join(END_ENTITY_CERT),
            b"I am not real end entity cert either",
        ).unwrap();

        rt.spawn(bg);

        fs::remove_dir_all(&data_path).unwrap();
        symlink(&real_data_path_2, &data_path).unwrap();

        let (item, _) = next_change(&mut rt, watch).unwrap();
        assert!(item.is_some());
        println!("saw first change");
    }


    #[test]
    fn polling_detects_create() {
        Fixture::new().test_polling(test_detects_create)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_create() {
        Fixture::new().test_inotify(test_detects_create)
    }

    #[test]
    #[cfg(not(target_os = "windows"))]
    fn polling_detects_create_symlink() {
        Fixture::new().test_polling(test_detects_create_symlink)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_create_symlink() {
        Fixture::new().test_inotify(test_detects_create_symlink)
    }

    #[test]
    #[cfg(not(target_os = "windows"))]
    fn polling_detects_create_double_symlink() {
        Fixture::new().test_polling(test_detects_create_double_symlink)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_create_double_symlink() {
        Fixture::new().test_inotify(test_detects_create_double_symlink)
    }

    #[test]
    fn polling_detects_modification() {
        Fixture::new().test_polling(test_detects_modification)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_modification() {
        Fixture::new().test_inotify(test_detects_modification)
    }

    #[test]
    #[cfg(not(target_os = "windows"))]
    fn polling_detects_modification_symlink() {
        Fixture::new().test_polling(test_detects_modification_symlink)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_modification_symlink() {
        Fixture::new().test_inotify(test_detects_modification_symlink)
    }

    #[test]
    #[cfg(not(target_os = "windows"))]
    fn polling_detects_modification_double_symlink() {
        Fixture::new().test_polling(test_detects_modification_double_symlink)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_modification_double_symlink() {
        Fixture::new().test_inotify(test_detects_modification_double_symlink)
    }

    #[test]
    #[cfg(not(target_os = "windows"))]
    fn polling_detects_double_symlink_retargeting() {
        Fixture::new().test_polling(test_detects_double_symlink_retargeting)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_double_symlink_retargeting() {
        Fixture::new().test_inotify(test_detects_double_symlink_retargeting)
    }

    #[test]
    fn polling_detects_delete_and_recreate() {
        Fixture::new().test_polling(test_detects_delete_and_recreate)
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn inotify_detects_delete_and_recreate() {
        Fixture::new().test_inotify(test_detects_delete_and_recreate)
    }

}
