// These crates are only used within the `tls` module.
extern crate ring;
extern crate rustls;
extern crate tokio_rustls;
extern crate untrusted;
extern crate webpki;

use std::fs::File;
use std::io::{self, Cursor, Read};
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;

use bytes::Buf;
use futures::Future;
use tokio::prelude::*;
use tokio::net::TcpStream;

use self::rustls::ServerSession;
use self::tokio_rustls::ServerConfigExt;

use transport::{AddrInfo, Io};

mod cert_resolver;

#[derive(Debug)]
pub struct ServerSettings {
    /// The trust anchors as concatenated PEM-encoded X.509 certificates.
    pub trust_anchors: PathBuf,

    /// The end-entity certificate as a DER-encoded X.509 certificate.
    pub end_entity_cert: PathBuf,

    /// The RSA private key in PEM-encoded PKCS#8 form.
    pub private_key: PathBuf,
}

#[derive(Clone)]
pub struct ServerConfig(Arc<rustls::ServerConfig>);

#[derive(Debug)]
pub enum ConfigError {
    Io(PathBuf, io::Error),
    FailedToParsePrivateKey,
    FailedToParseTrustAnchors(Option<webpki::Error>),
    EmptyEndEntityCert,
    EndEntityCertIsNotValid(webpki::Error),
    InvalidPrivateKey,
    TimeConversionFailed,
}

impl ServerSettings {
    pub fn load_from_disk(&self) -> Result<ServerConfig, ConfigError> {
        let trust_anchor_certs = load_file_contents(&self.trust_anchors)
            .and_then(|file_contents|
                rustls::internal::pemfile::certs(&mut Cursor::new(file_contents))
                    .map_err(|()| ConfigError::FailedToParseTrustAnchors(None)))?;
        let mut trust_anchors = Vec::with_capacity(trust_anchor_certs.len());
        for ta in &trust_anchor_certs {
            let ta = webpki::trust_anchor_util::cert_der_as_trust_anchor(
                untrusted::Input::from(ta.as_ref()))
                .map_err(|e| ConfigError::FailedToParseTrustAnchors(Some(e)))?;
            trust_anchors.push(ta);
        }
        let trust_anchors = webpki::TLSServerTrustAnchors(&trust_anchors);

        let end_entity_cert = load_file_contents(&self.end_entity_cert)?;

        // XXX: Assume there are no intermediates since there is no way to load
        // them yet.
        let cert_chain = vec![rustls::Certificate(end_entity_cert)];

        // Load the private key after we've validated the certificate.
        let private_key = load_file_contents(&self.private_key)?;
        let private_key = untrusted::Input::from(&private_key);

        let cert_resolver =
            cert_resolver::CertResolver::new(&trust_anchors, cert_chain, private_key)?;

        let mut config = rustls::ServerConfig::new(Arc::new(rustls::NoClientAuth));
        set_common_settings(&mut config.versions);
        config.cert_resolver = Arc::new(cert_resolver);

        Ok(ServerConfig(Arc::new(config)))
    }
}

pub fn accept_tls_connection(socket: TcpStream, ServerConfig(config): ServerConfig)
                             -> impl Future<Item = Connection, Error = io::Error>
{
    config.accept_async(socket).map(Connection)
}

// In theory we could replace `TcpStream` with `Io`. However, it is likely that
// in the future we'll need to do things specific to `TcpStream`, so optimize
// for that unless/until there is some benefit to doing otherwise.
#[derive(Debug)]
pub struct Connection(tokio_rustls::TlsStream<TcpStream, ServerSession>);

impl io::Read for Connection {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.0.read(buf)
    }
}

impl AsyncRead for Connection {
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.0.prepare_uninitialized_buffer(buf)
    }
}

impl io::Write for Connection {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.0.write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.0.flush()
    }
}

impl AsyncWrite for Connection {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        self.0.shutdown()
    }

    fn write_buf<B: Buf>(&mut self, buf: &mut B) -> Poll<usize, io::Error> {
        self.0.write_buf(buf)
    }
}

impl AddrInfo for Connection {
    fn local_addr(&self) -> Result<SocketAddr, io::Error> {
        self.0.get_ref().0.local_addr()
    }

    fn get_original_dst(&self) -> Option<SocketAddr> {
        self.0.get_ref().0.get_original_dst()
    }
}

impl Io for Connection {
    fn shutdown_write(&mut self) -> Result<(), io::Error> {
        self.0.get_mut().0.shutdown_write()
    }
}

fn set_common_settings(versions: &mut Vec<rustls::ProtocolVersion>) {
    // Only enable TLS 1.2 until TLS 1.3 is stable.
    *versions = vec![rustls::ProtocolVersion::TLSv1_2]
}

fn load_file_contents(path: &PathBuf) -> Result<Vec<u8>, ConfigError> {
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
        .map_err(|e| ConfigError::Io(path.clone(), e))
}
