use Body;
use flush::Flush;

use futures::{Future, Poll};
use h2::client::Connection;
use tokio_connect::Connect;

/// Task that performs background tasks for a client.
///
/// This is not used directly by a user of this library.
pub struct Background<C, S>
where C: Connect,
      S: Body,
{
    task: Task<C, S>,
}

/// The specific task to execute
enum Task<C, S>
where C: Connect,
      S: Body,
{
    Connection(Connection<C::Connected, S::Data>),
    Flush(Flush<S>),
}

// ===== impl Background =====

impl<C, S> Background<C, S>
where C: Connect,
      S: Body,
{
    pub(crate) fn connection(
        connection: Connection<C::Connected, S::Data>)
        -> Self
    {
        let task = Task::Connection(connection);
        Background { task }
    }

    pub(crate) fn flush(flush: Flush<S>) -> Self {
        let task = Task::Flush(flush);
        Background { task }
    }
}

impl<C, S> Future for Background<C, S>
where C: Connect,
      S: Body,
{
    type Item = ();
    type Error = ();

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        use self::Task::*;

        match self.task {
            // TODO: Log error?
            Connection(ref mut f) => f.poll().map_err(|_| ()),
            Flush(ref mut f) => f.poll(),
        }
    }
}
