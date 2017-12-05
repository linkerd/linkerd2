use Body;

use futures::{Future, Poll, Async};
use h2::{self, SendStream};
use http::HeaderMap;

/// Flush a body to the HTTP/2.0 send stream
pub(crate) struct Flush<S>
where S: Body,
{
    h2: SendStream<S::Data>,
    body: S,
    state: FlushState,
}

enum FlushState {
    Data,
    Trailers,
    Done,
}

enum DataOrTrailers<B> {
    Data(B),
    Trailers(HeaderMap),
}

// ===== impl Flush =====

impl<S> Flush<S>
where S: Body,
{
    pub fn new(src: S, dst: SendStream<S::Data>) -> Self {
        Flush {
            h2: dst,
            body: src,
            state: FlushState::Data,
        }
    }

    /// Try to flush the body.
    fn poll_complete(&mut self) -> Poll<(), h2::Error> {
        let mut first = try_ready!(self.poll_body());

        loop {
            if let Some(DataOrTrailers::Data(buf)) = first {
                let second = self.poll_body()?;
                let eos = if let Async::Ready(None) = second {
                    true
                } else {
                    false
                };
                self.h2.send_data(buf, eos)?;
                if eos {
                    return Ok(Async::Ready(()));
                } else if let Async::Ready(item) = second {
                    first = item;
                } else {
                    return Ok(Async::NotReady);
                }
            } else if let Some(DataOrTrailers::Trailers(trailers)) = first {
                self.h2.send_trailers(trailers)?;
                return Ok(Async::Ready(()));
            } else {
                return Ok(Async::Ready(()));
            }
        }
    }

    /// Get the next message to write, either a data frame or trailers.
    fn poll_body(&mut self) -> Poll<Option<DataOrTrailers<S::Data>>, h2::Error> {
        loop {
            match self.state {
                FlushState::Data => {
                    if let Some(data) = try_ready!(self.body.poll_data()) {
                        return Ok(Async::Ready(Some(DataOrTrailers::Data(data))));
                    } else {
                        self.state = FlushState::Trailers;
                    }
                }
                FlushState::Trailers => {
                    let trailers = try_ready!(self.body.poll_trailers());
                    self.state = FlushState::Done;
                    if let Some(trailers) = trailers {
                        return Ok(Async::Ready(Some(DataOrTrailers::Trailers(trailers))));
                    }
                }
                FlushState::Done => return Ok(Async::Ready(None)),
            }
        }
    }
}

impl<S> Future for Flush<S>
where S: Body,
{
    type Item = ();
    type Error = ();

    fn poll(&mut self) -> Poll<(), ()> {
        // TODO: Do something with the error
        self.poll_complete().map_err(|_| ())
    }
}
