use h2;
use bytes::IntoBuf;
use futures::{Async, Poll};
use http::HeaderMap;

/// A generic h2 client/server request/response body.
pub trait Body {
    /// The body chunk type
    type Data: IntoBuf + 'static;

    /// Returns `true` when the end of stream has been reached.
    ///
    /// An end of stream means that both `poll_data` and `poll_trailers` will
    /// return `None`.
    ///
    /// A return value of `false` **does not** guarantee that a value will be
    /// returend from `poll_stream` or `poll_trailers`.
    fn is_end_stream(&self) -> bool {
        false
    }

    /// Polls a stream of data.
    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error>;

    /// Returns possibly **one** `HeaderMap` for trailers.
    fn poll_trailers(&mut self) -> Poll<Option<HeaderMap>, h2::Error> {
        Ok(Async::Ready(None))
    }
}

impl Body for () {
    type Data = &'static [u8];

    #[inline]
    fn is_end_stream(&self) -> bool {
        true
    }

    #[inline]
    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        Ok(Async::Ready(None))
    }

    #[inline]
    fn poll_trailers(&mut self) -> Poll<Option<HeaderMap>, h2::Error> {
        Ok(Async::Ready(None))
    }
}
