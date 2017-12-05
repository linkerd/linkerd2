use Body;
use bytes::{Bytes, BytesMut, Buf};
use futures::{Poll, Stream};
use h2;
use http;

/// Allows a stream to be read from the remote.
#[derive(Debug, Default)]
pub struct RecvBody {
    inner: Option<h2::RecvStream>,
}

#[derive(Debug)]
pub struct Data {
    release_capacity: h2::ReleaseCapacity,
    bytes: Bytes,
}

// ===== impl RecvBody =====

impl RecvBody {
    /// Return a new `RecvBody`.
    pub(crate) fn new(inner: h2::RecvStream) -> Self {
        RecvBody { inner: Some(inner) }
    }
}

impl Body for RecvBody {
    type Data = Data;

    #[inline]
    fn is_end_stream(&self) -> bool {
        match self.inner {
            Some(ref inner) => inner.is_end_stream(),
            None => true,
        }
    }

    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        match self.inner {
            Some(ref mut inner) => {
                let data = try_ready!(inner.poll())
                    .map(|bytes| {
                        Data {
                            release_capacity: inner.release_capacity().clone(),
                            bytes,
                        }
                    });

                Ok(data.into())
            }
            None => Ok(None.into()),
        }
    }

    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
        match self.inner {
            Some(ref mut inner) => inner.poll_trailers(),
            None => Ok(None.into()),
        }
    }
}

// ===== impl Data =====

impl Buf for Data {
    fn remaining(&self) -> usize {
        self.bytes.len()
    }

    fn bytes(&self) -> &[u8] {
        self.bytes.as_ref()
    }

    fn advance(&mut self, cnt: usize) {
        if cnt > self.remaining() {
            panic!("advanced past end of buffer");
        }

        trace!("releasing capacity: {} of {}", cnt, self.remaining());
        let _ = self.bytes.split_to(cnt);

        self.release_capacity.release_capacity(cnt)
            .expect("flow control error")
    }
}

impl Drop for Data {
    fn drop(&mut self) {
        let sz = self.remaining();
        trace!("Data::drop: releasing capacity: {}", sz);
        self.release_capacity
            .release_capacity(sz)
            .expect("flow control error");
    }
}

impl From<Data> for Bytes {
    fn from(mut src: Data) -> Self {
        let bytes = ::std::mem::replace(&mut src.bytes, Bytes::new());

        src.release_capacity.release_capacity(bytes.len())
            .expect("flow control error");

        bytes
    }
}

impl From<Data> for BytesMut {
    fn from(mut src: Data) -> Self {
        let bytes = ::std::mem::replace(&mut src.bytes, Bytes::new());

        src.release_capacity.release_capacity(bytes.len())
            .expect("flow control error");

        bytes.into()
    }
}
