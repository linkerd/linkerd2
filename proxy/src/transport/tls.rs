extern crate rustls;
extern crate tokio_rustls;

use std::fs::File;
use std::io::{self, Cursor, Read};
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;

use bytes::Buf;
use futures::Future;
use tokio::prelude::*;
use tokio::net::TcpStream;

use self::rustls::{ServerSession, internal::pemfile};
use self::tokio_rustls::ServerConfigExt;

use transport::{AddrInfo, Io};

#[derive(Debug)]
pub struct ServerSettings {
    /// The certificate chain, sans root certificate, encoded as the
    /// concatenation of PEM-encoded DER X.509 certificates, end-entity
    /// certificate first. (TODO).
    cert_chain: PathBuf,

    /// The RSA private key in PEM-encoded PKCS#8 form.
    private_key: PathBuf,
}

#[derive(Clone)]
pub struct ServerConfig(Arc<rustls::ServerConfig>);

#[derive(Debug)]
pub enum ConfigError {
    Io(PathBuf, io::Error),
    FailedToParseCertChain,
    FailedToParsePrivateKey,
    FailedToParseCaBundle,
    EmptyCertChain,
}

impl ServerSettings {
    pub fn from_cert_chain_and_private_key(cert_chain: PathBuf, private_key: PathBuf) -> Self {
        Self {
            cert_chain,
            private_key,
        }
    }

    pub fn load_from_disk(&self) -> Result<ServerConfig, ConfigError> {
        let cert_chain =
            pemfile::certs(&mut load_file_contents(&self.cert_chain)?)
                .map_err(|()| ConfigError::FailedToParseCertChain)?;
        let private_key = {
            let mut private_keys =
                pemfile::pkcs8_private_keys(&mut load_file_contents(&self.private_key)?)
                    .map_err(|()| ConfigError::FailedToParsePrivateKey)?;
            if !private_keys.len() != 1 {
                return Err(ConfigError::FailedToParsePrivateKey);
            }
            private_keys.pop().unwrap() // We just checked it.
        };

        let mut config = rustls::ServerConfig::new(Arc::new(rustls::NoClientAuth));
        set_common_settings(&mut config.versions);
        config.set_single_cert(cert_chain, private_key);

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

fn load_file_contents(path: &PathBuf) -> Result<Cursor<Vec<u8>>, ConfigError> {
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
            Cursor::new(contents)
        })
        .map_err(|e| ConfigError::Io(path.clone(), e))
}
