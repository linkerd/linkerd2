use {Body, RecvBody};
use super::{Service, Background};

use futures::{Future, Async, Poll};
use futures::future::Executor;
use h2;
use h2::client::Connection;
use http::{Request, Response};
use tokio_connect::Connect;

use std::boxed::Box;
use std::marker::PhantomData;

/// Establishes a Client on an H2 connection.
///
/// Has a builder-like API for configuring client connections.  Currently this only allows
/// the configuration of TLS transport on new services created by this factory.
pub struct Client<C, E, S> {
    /// Establish new session layer values (usually TCP sockets w/ TLS).
    connect: C,

    /// H2 client configuration
    builder: h2::client::Builder,

    /// Used to spawn connection management tasks and tasks to flush send
    /// body streams.
    executor: E,

    /// The HTTP request body type.
    _p: PhantomData<S>,
}

/// Completes with a Service when the H2 connection has been initialized.
pub struct ConnectFuture<C, E, S>
where C: Connect + 'static,
      S: Body + 'static,
{
    future: Box<Future<Item = Connected<S::Data, C::Connected>, Error = ConnectError<C::Error>>>,
    executor: E,
    _p: PhantomData<S>,
}

/// The type yielded by an h2 client handshake future
type Connected<S, C> = (h2::client::Client<S>, Connection<C, S>);

/// Error produced when establishing an H2 client connection.
#[derive(Debug)]
pub enum ConnectError<T> {
    /// An error occurred when attempting to establish the underlying session
    /// layer.
    Connect(T),

    /// An error occurred when attempting to perform the HTTP/2.0 handshake.
    Proto(h2::Error),

    /// An error occured when attempting to execute a worker task
    Execute,
}

// ===== impl Client =====

impl<C, E, S> Client<C, E, S>
where
    C: Connect,
    E: Executor<Background<C, S>> + Clone,
    S: Body,
{
    /// Create a new `Client`.
    ///
    /// The `connect` argument is used to obtain new session layer instances
    /// (`AsyncRead` + `AsyncWrite`). For each new client service returned, a
    /// task will be spawned onto `executor` that will be used to manage the H2
    /// connection.
    pub fn new(connect: C, builder: h2::client::Builder, executor: E) -> Self {
        Client {
            connect,
            executor,
            builder,
            _p: PhantomData,
        }
    }
}

impl<C, E, S> ::tower::NewService for Client<C, E, S>
where
    C: Connect + 'static,
    E: Executor<Background<C, S>> + Clone,
    S: Body + 'static,
{
    type Request = Request<S>;
    type Response = Response<RecvBody>;
    type Error = super::Error;
    type InitError = ConnectError<C::Error>;
    type Service = Service<C, E, S>;
    type Future = ConnectFuture<C, E, S>;

    /// Obtains a Service on a single plaintext h2 connection to a remote.
    fn new_service(&self) -> Self::Future {
        let client = self.builder.clone();
        let conn = self.connect.connect()
            .map_err(ConnectError::Connect)
            .and_then(move |io| {
                client
                    .handshake(io)
                    .map_err(ConnectError::Proto)
            });

        ConnectFuture {
            future: Box::new(conn),
            executor: self.executor.clone(),
            _p: PhantomData,
        }
    }
}

// ===== impl ConnectFuture =====

impl<C, E, S> Future for ConnectFuture<C, E, S>
where
    C: Connect,
    E: Executor<Background<C, S>> + Clone,
    S: Body,
{
    type Item = Service<C, E, S>;
    type Error = ConnectError<C::Error>;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        // Get the session layer instance
        let (client, connection) = try_ready!(self.future.poll());

        // Spawn the worker task
        let task = Background::connection(connection);
        self.executor.execute(task).map_err(|_| ConnectError::Execute)?;

        // Create an instance of the service
        let service = Service::new(client, self.executor.clone());

        Ok(Async::Ready(service))
    }
}
