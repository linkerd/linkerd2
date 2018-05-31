use std::io;
use std::net::Shutdown;
use tokio::io::{AsyncRead, AsyncWrite};
use tokio::net::TcpStream;

mod connect;
mod addr_info;
pub mod tls;

pub use self::connect::{
    Connect,
    DnsNameAndPort, Host, HostAndPort, HostAndPortError,
    LookupAddressAndConnect,
};
pub use self::addr_info::{AddrInfo, GetOriginalDst, SoOriginalDst};

pub trait Io: AddrInfo + AsyncRead + AsyncWrite + Send {
    fn shutdown_write(&mut self) -> Result<(), io::Error>;
}

impl Io for TcpStream {
    fn shutdown_write(&mut self) -> Result<(), io::Error> {
        TcpStream::shutdown(self, Shutdown::Write)
    }
}
