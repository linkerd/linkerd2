use std::collections::VecDeque;

use bytes::{Buf, BufMut, Bytes, BytesMut, BigEndian};
use futures::{Async, Stream, Poll};
use h2;
use http::header::HeaderMap;
use tower_h2::{self, Body, Data, RecvBody};

use ::Status;
use super::check_grpc_status;

/// A type used to encode and decode for a single RPC method.
pub trait Codec: Clone {
    /// The content-type header for messages using this encoding.
    ///
    /// Should be `application/grpc+yourencoding`.
    const CONTENT_TYPE: &'static str;

    /// The message to encode into bytes.
    type Encode;
    /// The message to decode from bytes.
    type Decode;
    /// An error that could occur during encoding.
    type EncodeError;
    /// An error that could occur during decoding.
    type DecodeError;

    /// Encode a message into the provided buffer.
    fn encode(&mut self, item: Self::Encode, buf: &mut EncodeBuf) -> Result<(), Self::EncodeError>;

    /// Decode a message from the buffer.
    ///
    /// The buffer will contain exactly the bytes of a full message. There
    /// is no need to get the length from the bytes, gRPC framing is handled
    /// for you.
    fn decode(&mut self, buf: &mut DecodeBuf) -> Result<Self::Decode, Self::DecodeError>;
}

/// A buffer to encode a message into.
#[derive(Debug)]
pub struct EncodeBuf<'a> {
    pub(crate) bytes: &'a mut BytesMut,
}

/// A buffer to decode messages from.
#[derive(Debug)]
pub struct DecodeBuf<'a> {
    pub(crate) bufs: &'a mut BytesList,
    pub(crate) len: usize,
}

/// A mapping of a stream of encodable items to a stream of bytes.
#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct EncodingBody<E, S> {
    buf: BytesMut,
    encoder: E,
    stream: S,
}

#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct DecodingBody<D> {
    bufs: BytesList,
    decoder: D,
    state: DecodingState,
    stream: RecvBody,
}

#[derive(Debug)]
enum DecodingState {
    ReadHeader,
    ReadBody {
        compression: bool,
        len: usize,
    },
    Trailers,
    Done,
}

#[derive(Debug)]
pub(crate) struct BytesList {
    pub(crate) bufs: VecDeque<Data>,
}

impl<'a> EncodeBuf<'a> {
    #[inline]
    pub fn reserve(&mut self, capacity: usize) {
        self.bytes.reserve(capacity);
    }
}

impl<'a> BufMut for EncodeBuf<'a> {
    #[inline]
    fn remaining_mut(&self) -> usize {
        self.bytes.remaining_mut()
    }

    #[inline]
    unsafe fn advance_mut(&mut self, cnt: usize) {
        self.bytes.advance_mut(cnt)
    }

    #[inline]
    unsafe fn bytes_mut(&mut self) -> &mut [u8] {
        self.bytes.bytes_mut()
    }
}

impl<'a> Buf for DecodeBuf<'a> {
    #[inline]
    fn remaining(&self) -> usize {
        self.len
    }

    #[inline]
    fn bytes(&self) -> &[u8] {
        &self.bufs.bytes()[..self.len]
    }

    #[inline]
    fn advance(&mut self, cnt: usize) {
        assert!(cnt <= self.len);
        self.bufs.advance(cnt);
        self.len -= cnt;
    }
}

impl<'a> Drop for DecodeBuf<'a> {
    fn drop(&mut self) {
        if self.len > 0 {
            warn!("DecodeBuf was not advanced to end");
            self.bufs.advance(self.len);
        }
    }
}

impl Buf for BytesList {
    #[inline]
    fn remaining(&self) -> usize {
        self.bufs.iter().map(|buf| buf.remaining()).sum()
    }

    #[inline]
    fn bytes(&self) -> &[u8] {
        if self.bufs.is_empty() {
            &[]
        } else {
            &self.bufs[0].bytes()
        }
    }

    #[inline]
    fn advance(&mut self, mut cnt: usize) {
        while cnt > 0 {
            {
                let front = &mut self.bufs[0];
                if front.remaining() > cnt {
                    front.advance(cnt);
                    return;
                } else {
                    cnt -= front.remaining();
                }
            }
            self.bufs.pop_front();
        }
    }
}

impl<E, S> EncodingBody<E, S> {
    pub(crate) fn new(encoder: E, stream: S) -> Self {
        EncodingBody {
            buf: BytesMut::new(),
            encoder,
            stream,
        }
    }
}

impl<E, S> tower_h2::Body for EncodingBody<E, S>
where
    S: Stream,
    E: Codec<Encode=S::Item>,
{
    type Data = Bytes;

    fn is_end_stream(&self) -> bool {
        false
    }

    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        let item = try_ready!(self.stream.poll().map_err(|_| h2_err()));
        if let Some(item) = item {
            self.buf.reserve(5);
            unsafe { self.buf.advance_mut(5); }
            self.encoder.encode(item, &mut EncodeBuf {
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
}

fn grpc_status(trailers: &HeaderMap) -> Result<(), Status> {
    match check_grpc_status(&trailers) {
        Some(status) => if status.code() == ::Code::OK {
            Ok(())
        } else {
            Err(status)
        }
        None => {
            trace!("trailers missing grpc-status");
            Err(Status::UNKNOWN)
        }
    }
}


impl<D> DecodingBody<D>
where
    D: Codec,
{
    pub(crate) fn new(decoder: D, stream: RecvBody) -> Self {
        DecodingBody {
            bufs: BytesList {
                bufs: VecDeque::new(),
            },
            decoder,
            state: DecodingState::ReadHeader,
            stream,
        }
    }

    fn decode(&mut self) -> Result<Option<D::Decode>, Status> {
        if let DecodingState::ReadHeader = self.state {
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

            self.state = DecodingState::ReadBody {
                compression: is_compressed,
                len,
            }
        }

        if let DecodingState::ReadBody { len, .. } = self.state {
            if self.bufs.remaining() < len {
                return Ok(None);
            }

            match self.decoder.decode(&mut DecodeBuf {
                bufs: &mut self.bufs,
                len,
            }) {
                Ok(msg) => {
                    self.state = DecodingState::ReadHeader;
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

    fn poll_inner(&mut self) -> Poll<Option<D::Decode>, ::Error<h2::Error>> {
        loop {
            match self.state {
                DecodingState::Trailers | DecodingState::Done => break,
                _ => (),
            }

            match self.decode() {
                Ok(Some(val)) => return Ok(Async::Ready(Some(val))),
                Ok(None) => (),
                Err(status) => return Err(::Error::Grpc(status)),
            }

            let chunk = try_ready!(self.stream.poll_data());

            if let Some(data) = chunk {
                self.bufs.bufs.push_back(data);
            } else {
                if self.bufs.has_remaining() {
                    trace!("unexpected EOF decoding stream");
                    return Err(::Error::Grpc(Status::UNKNOWN))
                } else {
                    self.state = DecodingState::Trailers;
                    break;
                }
            }
        }

        if let DecodingState::Trailers = self.state {
            return if let Some(trailers) = try_ready!(self.stream.poll_trailers()) {
                grpc_status(&trailers).map_err(::Error::Grpc)?;
                self.state = DecodingState::Done;
                Ok(Async::Ready(None))
            } else {
                trace!("receive body ended without trailers");
                Err(::Error::Grpc(Status::UNKNOWN))
            }
        }
        Ok(Async::Ready(None))
    }
}

impl<D> Stream for DecodingBody<D>
where
    D: Codec,
{
    type Item = D::Decode;
    type Error = ::Error<h2::Error>;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        self.poll_inner()
            .map_err(|err| {
                self.state = DecodingState::Done;
                err
            })
    }
}

fn h2_err() -> h2::Error {
    unimplemented!("EncodingBody map_err")
}

/// Wraps a message to provide a `Stream` of just one item.
#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct Unary<T> {
    item: Option<T>,
}

impl<T> Unary<T> {
    pub fn new(item: T) -> Self {
        Unary {
            item: Some(item),
        }
    }
}

impl<T> Stream for Unary<T> {
    type Item = T;
    type Error = self::inner::Void;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        Ok(Async::Ready(self.item.take()))
    }
}

mod inner {
    pub struct Void(Void_);
    enum Void_ {}
}
