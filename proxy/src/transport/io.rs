use std::io;
use std::net::{Shutdown, SocketAddr};

use bytes::Buf;
use futures::Poll;
use tokio::io::{AsyncRead, AsyncWrite};

use super::AddrInfo;
use self::internal::Io;

/// A public wrapper around a `Box<Io>`.
///
/// This type ensures that the proper write_buf method is called,
/// to allow vectored writes to occur.
#[derive(Debug)]
pub struct BoxedIo(Box<Io>);

impl BoxedIo {
    pub fn new<T: Io + 'static>(io: T) -> Self {
        BoxedIo(Box::new(io))
    }

    /// Since `Io` isn't publicly exported, but `Connection` wants
    /// this method, it's just an inherent method.
    pub fn shutdown_write(&mut self) -> Result<(), io::Error> {
        self.0.shutdown_write()
    }
}

impl io::Read for BoxedIo {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.0.read(buf)
    }
}

impl io::Write for BoxedIo {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.0.write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.0.flush()
    }
}

impl AsyncRead for BoxedIo {
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.0.prepare_uninitialized_buffer(buf)
    }
}

impl AsyncWrite for BoxedIo {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        self.0.shutdown()
    }

    fn write_buf<B: Buf>(&mut self, mut buf: &mut B) -> Poll<usize, io::Error> {
        // A trait object of AsyncWrite would use the default write_buf,
        // which doesn't allow vectored writes. Going through this method
        // allows the trait object to call the specialized write_buf method.
        self.0.write_buf_erased(&mut buf)
    }
}

impl AddrInfo for BoxedIo {
    fn local_addr(&self) -> Result<SocketAddr, io::Error> {
        self.0.local_addr()
    }

    fn get_original_dst(&self) -> Option<SocketAddr> {
        self.0.get_original_dst()
    }
}

pub(super) mod internal {
    use std::io;
    use tokio::net::TcpStream;
    use super::{AddrInfo, AsyncRead, AsyncWrite, Buf, Poll, Shutdown};

    /// This trait is private, since it's purpose is for creating a dynamic
    /// trait object, but doing so without care can lead not getting vectored
    /// writes.
    ///
    /// Instead, used the concrete `BoxedIo` type.
    pub trait Io: AddrInfo + AsyncRead + AsyncWrite + Send {
        fn shutdown_write(&mut self) -> Result<(), io::Error>;

        /// This method is to allow using `Async::write_buf` even through a
        /// trait object.
        fn write_buf_erased(&mut self, buf: &mut Buf) -> Poll<usize, io::Error>;
    }

    impl Io for TcpStream {
        fn shutdown_write(&mut self) -> Result<(), io::Error> {
            TcpStream::shutdown(self, Shutdown::Write)
        }

        fn write_buf_erased(&mut self, mut buf: &mut Buf) -> Poll<usize, io::Error> {
            self.write_buf(&mut buf)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Debug)]
    struct WriteBufDetector;

    impl io::Read for WriteBufDetector {
        fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
            unimplemented!()
        }
    }

    impl io::Write for WriteBufDetector {
        fn write(&mut self, _: &[u8]) -> io::Result<usize> {
            panic!("BoxedIo called wrong write_buf method");
        }
        fn flush(&mut self) -> io::Result<()> {
            unimplemented!()
        }
    }

    impl AsyncRead for WriteBufDetector {}

    impl AsyncWrite for WriteBufDetector {
        fn shutdown(&mut self) -> Poll<(), io::Error> {
            unimplemented!()
        }

        fn write_buf<B: Buf>(&mut self, _: &mut B) -> Poll<usize, io::Error> {
            Ok(0.into())
        }
    }

    impl AddrInfo for WriteBufDetector {
        fn local_addr(&self) -> Result<SocketAddr, io::Error> {
            unimplemented!()
        }

        fn get_original_dst(&self) -> Option<SocketAddr> {
            unimplemented!()
        }
    }

    impl Io for WriteBufDetector {
        fn shutdown_write(&mut self) -> Result<(), io::Error> {
            unimplemented!()
        }

        fn write_buf_erased(&mut self, mut buf: &mut Buf) -> Poll<usize, io::Error> {
            self.write_buf(&mut buf)
        }
    }


    #[test]
    fn boxed_io_uses_vectored_io() {
        use bytes::IntoBuf;
        let mut io = BoxedIo::new(WriteBufDetector);

        // This method will trigger the panic in WriteBufDetector::write IFF
        // BoxedIo doesn't call write_buf_erased, but write_buf, and triggering
        // a regular write.
        io.write_buf(&mut "hello".into_buf()).expect("write_buf");
    }
}
