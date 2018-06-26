use std::{cmp, fmt::Debug, io, net::SocketAddr};

use super::io::internal::Io;
use bytes::{Buf, Bytes};
use tokio::prelude::*;
use AddrInfo;

/// A TcpStream where the initial reads will be served from `prefix`.
#[derive(Debug)]
pub struct Prefixed<S> {
    prefix: Bytes,
    io: S,
}

impl<S> Prefixed<S> {
    pub fn new(prefix: Bytes, io: S) -> Self {
        Self { prefix, io }
    }
}

impl<S> io::Read for Prefixed<S> where S: Debug + io::Read {
    fn read(&mut self, buf: &mut [u8]) -> Result<usize, io::Error> {
        // Check the length only once, since looking as the length
        // of a Bytes isn't as cheap as the length of a &[u8].
        let peeked_len = self.prefix.len();

        if peeked_len == 0 {
            self.io.read(buf)
        } else {
            let len = cmp::min(buf.len(), peeked_len);
            buf[..len].copy_from_slice(&self.prefix.as_ref()[..len]);
            self.prefix.advance(len);
            // If we've finally emptied the peek_buf, drop it so we don't
            // hold onto the allocated memory any longer. We won't peek
            // again.
            if peeked_len == len {
                self.prefix = Default::default();
            }
            Ok(len)
        }
    }
}

impl<S> AsyncRead for Prefixed<S> where S: AsyncRead + Debug {
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.io.prepare_uninitialized_buffer(buf)
    }
}

impl<S> io::Write for Prefixed<S> where S: Debug + io::Write {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.io.write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.io.flush()
    }
}

impl<S> AsyncWrite for Prefixed<S> where S: AsyncWrite + Debug {
    fn shutdown(&mut self) -> Result<Async<()>, io::Error> {
        self.io.shutdown()
    }

    fn write_buf<B: Buf>(&mut self, buf: &mut B) -> Poll<usize, io::Error>
        where Self: Sized
    {
        self.io.write_buf(buf)
    }
}

impl<S> AddrInfo for Prefixed<S> where S: AddrInfo {
    fn local_addr(&self) -> Result<SocketAddr, io::Error> {
        self.io.local_addr()
    }

    fn get_original_dst(&self) -> Option<SocketAddr> {
        self.io.get_original_dst()
    }
}

impl<S> Io for Prefixed<S> where S: Io {
    fn shutdown_write(&mut self) -> Result<(), io::Error> {
        self.io.shutdown_write()
    }

    fn write_buf_erased(&mut self, buf: &mut Buf) -> Result<Async<usize>, io::Error> {
        self.io.write_buf_erased(buf)
    }
}
