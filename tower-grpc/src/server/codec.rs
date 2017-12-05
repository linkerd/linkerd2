use Status;
// TODO: These types will most likely be moved back to the top level.
use client::codec::{DecodeBuf, EncodeBuf, BytesList};

use bytes::{Buf, BufMut, BytesMut, Bytes, BigEndian};
use futures::{Stream, Poll, Async};
use h2;
use http::HeaderMap;
use tower_h2::{self, Body};

use std::collections::VecDeque;

/// Encodes and decodes gRPC message types
pub trait Codec {
    /// The content-type header for messages using this encoding.
    ///
    /// Should be `application/grpc+yourencoding`.
    const CONTENT_TYPE: &'static str;

    /// The encode type
    type Encode;

    /// Encoder type
    type Encoder: Encoder<Item = Self::Encode>;

    /// The decode type
    type Decode;

    /// Decoder type
    type Decoder: Decoder<Item = Self::Decode>;

    /// Returns a new encoder
    fn encoder(&mut self) -> Self::Encoder;

    /// Returns a new decoder
    fn decoder(&mut self) -> Self::Decoder;
}

/// Encodes gRPC message types
pub trait Encoder {
    /// Type that is encoded
    type Item;

    /// Encode a message into the provided buffer.
    fn encode(&mut self, item: Self::Item, buf: &mut EncodeBuf) -> Result<(), ::Error>;
}

/// Decodes gRPC message types
pub trait Decoder {
    /// Type that is decoded
    type Item;

    /// Decode a message from the buffer.
    ///
    /// The buffer will contain exactly the bytes of a full message. There
    /// is no need to get the length from the bytes, gRPC framing is handled
    /// for you.
    fn decode(&mut self, buf: &mut DecodeBuf) -> Result<Self::Item, ::Error>;
}

/// Encodes gRPC message types
#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct Encode<T, E> {
    inner: EncodeInner<T, E>,

    /// Destination buffer
    buf: BytesMut,
}

#[derive(Debug)]
enum EncodeInner<T, E> {
    Ok {
        /// The source of messages to encode
        inner: T,

        /// The encoder
        encoder: E,
    },
    Err(Status),
}

/// Decodes gRPC message types
#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct Decode<D> {
    /// The source of encoded messages
    inner: tower_h2::RecvBody,

    /// The decoder
    decoder: D,

    /// buffer
    bufs: BytesList,

    /// Decoding state
    state: State,
}

#[derive(Debug)]
enum State {
    ReadHeader,
    ReadBody {
        compression: bool,
        len: usize,
    },
    Done,
}

// ===== impl Encode =====

impl<T, E> Encode<T, E>
where T: Stream,
      E: Encoder<Item = T::Item>,
{
    pub(crate) fn new(inner: T, encoder: E) -> Self {
        Encode {
            inner: EncodeInner::Ok { inner, encoder },
            buf: BytesMut::new(),
        }
    }

    pub(crate) fn error(status: Status) -> Self {
        Encode {
            inner: EncodeInner::Err(status),
            buf: BytesMut::new(),
        }
    }
}

impl<T, E> tower_h2::Body for Encode<T, E>
where T: Stream,
      E: Encoder<Item = T::Item>,
{
    type Data = Bytes;

    fn is_end_stream(&self) -> bool {
        false
    }

    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        match self.inner {
            EncodeInner::Ok { ref mut inner, ref mut encoder } => {
                let item = try_ready!(inner.poll().map_err(|_| h2_err()));

                if let Some(item) = item {
                    self.buf.reserve(5);
                    unsafe { self.buf.advance_mut(5); }
                    encoder.encode(item, &mut EncodeBuf {
                        bytes: &mut self.buf,
                    }).map_err(|_| h2_err())?;

                    // now that we know length, we can write the header
                    let len = self.buf.len() - 5;
                    assert!(len <= ::std::u32::MAX as usize);
                    {
                        let mut cursor = ::std::io::Cursor::new(&mut self.buf[..5]);
                        cursor.put_u8(0); // byte must be 0, reserve doesn't auto-zero
                        cursor.put_u32::<BigEndian>(len as u32);
                    }

                    Ok(Async::Ready(Some(self.buf.split_to(len + 5).freeze())))
                } else {
                    Ok(Async::Ready(None))
                }
            }
            _ => Ok(Async::Ready(None)),
        }
    }

    fn poll_trailers(&mut self) -> Poll<Option<HeaderMap>, h2::Error> {
        let mut map = HeaderMap::new();

        let status = match self.inner {
            EncodeInner::Ok { .. } => Status::OK.to_header_value(),
            EncodeInner::Err(ref status) => status.to_header_value(),
        };

        // Success
        map.insert("grpc-status", status);

        Ok(Some(map).into())
    }
}

// ===== impl Decode =====

impl<D> Decode<D>
where D: Decoder,
{
    pub(crate) fn new(inner: tower_h2::RecvBody, decoder: D) -> Self {
        Decode {
            inner,
            decoder,
            bufs: BytesList {
                bufs: VecDeque::new(),
            },
            state: State::ReadHeader,
        }
    }

    fn decode(&mut self) -> Result<Option<D::Item>, Status> {
        if let State::ReadHeader = self.state {
            if self.bufs.remaining() < 5 {
                return Ok(None);
            }

            let is_compressed = match self.bufs.get_u8() {
                0 => false,
                1 => {
                    trace!("message compressed, compression not supported yet");
                    return Err(Status::UNIMPLEMENTED);
                },
                _ => {
                    trace!("unexpected compression flag");
                    return Err(Status::UNKNOWN);
                }
            };
            let len = self.bufs.get_u32::<BigEndian>() as usize;

            self.state = State::ReadBody {
                compression: is_compressed,
                len,
            }
        }

        if let State::ReadBody { len, .. } = self.state {
            if self.bufs.remaining() < len {
                return Ok(None);
            }

            match self.decoder.decode(&mut DecodeBuf {
                bufs: &mut self.bufs,
                len,
            }) {
                Ok(msg) => {
                    self.state = State::ReadHeader;
                    return Ok(Some(msg));
                },
                Err(_) => {
                    debug!("decoder error");
                    return Err(Status::UNKNOWN);
                }
            }
        }

        Ok(None)
    }
}

impl<D> Stream for Decode<D>
where D: Decoder,
{
    type Item = D::Item;
    type Error = ::Error;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        loop {
            if let State::Done = self.state {
                break;
            }

            match self.decode() {
                Ok(Some(val)) => return Ok(Async::Ready(Some(val))),
                Ok(None) => (),
                Err(status) => return Err(::Error::Grpc(status)),
            }

            let chunk = try_ready!(self.inner.poll_data());

            if let Some(data) = chunk {
                self.bufs.bufs.push_back(data);
            } else {
                if self.bufs.has_remaining() {
                    trace!("unexpected EOF decoding stream");
                    return Err(::Error::Grpc(Status::UNKNOWN))
                } else {
                    self.state = State::Done;
                    break;
                }
            }
        }

        if let Some(trailers) = try_ready!(self.inner.poll_trailers()) {
            grpc_status(&trailers).map_err(::Error::Grpc)?;
            Ok(Async::Ready(None))
        } else {
            trace!("receive body ended without trailers");
            Err(::Error::Grpc(Status::UNKNOWN))
        }
    }
}


// ===== impl utils =====

fn h2_err() -> h2::Error {
    unimplemented!("EncodingBody map_err")
}

fn grpc_status(trailers: &HeaderMap) -> Result<(), Status> {
    if let Some(status) = trailers.get("grpc-status") {
        let status = Status::from_bytes(status.as_ref());
        if status.code() == ::Code::OK {
            Ok(())
        } else {
            Err(status)
        }
    } else {
        trace!("trailers missing grpc-status");
        Err(Status::UNKNOWN)
    }
}
