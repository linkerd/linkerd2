use std::fmt;
use std::marker::PhantomData;

use bytes::{Buf, BufMut};
use prost::{DecodeError, Message};
use tower_grpc::client::codec::{Codec, DecodeBuf, EncodeBuf};

/// A protobuf codec.
pub struct Protobuf<T, U>(PhantomData<(T, U)>);

impl<T, U> Protobuf<T, U> {
    pub fn new() -> Self {
        Protobuf(PhantomData)
    }
}

impl<T, U> Clone for Protobuf<T, U> {
    fn clone(&self) -> Self {
        Protobuf(PhantomData)
    }
}

impl<T, U> fmt::Debug for Protobuf<T, U> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("Protobuf")
    }
}

impl<T: Message, U: Message + Default> Codec for Protobuf<T, U> {
    const CONTENT_TYPE: &'static str = "application/grpc+proto";

    type Encode = T;
    type Decode = U;
    // never errors
    type EncodeError = Void;
    type DecodeError = DecodeError;

    fn encode(&mut self, msg: Self::Encode, buf: &mut EncodeBuf) -> Result<(), Self::EncodeError> {
        let len = msg.encoded_len();
        if buf.remaining_mut() < len {
            buf.reserve(len);
        }
        // prost says the only error from `Message::encode` is if there is not
        // enough space in the buffer.
        msg.encode(buf).expect("buf space was reserved");
        Ok(())
    }

    fn decode(&mut self, buf: &mut DecodeBuf) -> Result<Self::Decode, Self::DecodeError> {
        trace!("decode; bytes={}", buf.remaining());

        match Message::decode(buf) {
            Ok(msg) => Ok(msg),
            Err(err) => {
                debug!("decode error: {:?}", err);
                Err(err)
            }
        }
    }
}

/// Can never be instantiated.
pub enum Void {}
