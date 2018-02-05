extern crate futures;

use std::fmt;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};

use futures::{Async, AsyncSink, Poll, Sink, StartSend, Stream};
use futures::sync::mpsc;

/// Creates a lossy multi-producer single-consumer channel.
///
/// This channel is bounded but provides no mechanism for backpressure. Though it returns
/// items that it cannot accept, it does not notify a producer of capacity availability.
///
/// This allows producers to send events on this channel without obtaining a mutable
/// reference to a sender.
pub fn channel<T>(capacity: usize) -> (Sender<T>, Receiver<T>) {
    let (tx, rx) = mpsc::unbounded();
    let capacity = Arc::new(AtomicUsize::new(capacity));

    let s = Sender {
        tx,
        capacity: capacity.clone(),
    };

    let r = Receiver {
        rx,
        capacity,
    };

    (s, r)
}

pub struct Receiver<T> {
    rx: mpsc::UnboundedReceiver<T>,
    capacity: Arc<AtomicUsize>,
}

pub struct Sender<T> {
    tx: mpsc::UnboundedSender<T>,
    capacity: Arc<AtomicUsize>,
}

/// Indicates that channel was not able to send an item. Subsequents items, however, may
/// be sent iff the item is `Rejected`.
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub enum SendError<T> {
    NoReceiver(T),
    Rejected(T),
}

// ===== impl Receiver =====

impl<T> Stream for Receiver<T> {
    type Item = T;
    type Error = ();

    fn poll(&mut self) -> Poll<Option<T>, Self::Error> {
        match self.rx.poll() {
            Ok(Async::Ready(Some(v))) => {
                self.capacity.fetch_add(1, Ordering::SeqCst);
                Ok(Async::Ready(Some(v)))
            }
            res => res,
        }
    }
}

// NB: `rx` does not have a `Debug` impl.
impl<T> fmt::Debug for Receiver<T> {
    fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
        fmt.debug_struct("Receiver")
            .field("capacity", &self.capacity)
            .finish()
    }
}

// ===== impl Sender =====

impl<T> Sender<T> {
    pub fn lossy_send(&self, v: T) -> Result<(), SendError<T>> {
        loop {
            let cap = self.capacity.load(Ordering::SeqCst);
            if cap == 0 {
                return Err(SendError::Rejected(v));
            }

            let ret = self.capacity
                .compare_and_swap(cap, cap - 1, Ordering::SeqCst);
            if ret == cap {
                break;
            }
        }

        self.tx
            .unbounded_send(v)
            .map_err(|se| SendError::NoReceiver(se.into_inner()))
    }
}

/// Drops events instead of exerting backpressure
impl<T> Sink for Sender<T> {
    type SinkItem = T;
    type SinkError = SendError<T>;

    fn start_send(&mut self, item: T) -> StartSend<Self::SinkItem, Self::SinkError> {
        self.lossy_send(item).map(|_| AsyncSink::Ready)
    }

    fn poll_complete(&mut self) -> Poll<(), Self::SinkError> {
        Ok(().into())
    }
}

// NB Clone cannot be derived because `T` doesn't have to implement Clone.
impl<T> Clone for Sender<T> {
    fn clone(&self) -> Self {
        Sender {
            tx: self.tx.clone(),
            capacity: self.capacity.clone(),
        }
    }
}

// NB: `tx` does not have a `Debug` impl.
impl<T> fmt::Debug for Sender<T> {
    fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
        fmt.debug_struct("Sender")
            .field("capacity", &self.capacity)
            .finish()
    }
}

// ===== impl SendError =====

impl<T> SendError<T> {
    pub fn into_inner(self) -> T {
        match self {
            SendError::NoReceiver(v) | SendError::Rejected(v) => v,
        }
    }
}
