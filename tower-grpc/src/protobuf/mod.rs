use client::codec::{EncodeBuf, DecodeBuf};

use bytes::{BufMut};
use prost::Message;

use ::std::marker::PhantomData;

/// Protobuf codec
#[derive(Debug)]
pub struct Codec<T, U>(PhantomData<(T, U)>);

#[derive(Debug)]
pub struct Encoder<T>(PhantomData<T>);

#[derive(Debug)]
pub struct Decoder<T>(PhantomData<T>);

/// Protobuf gRPC type aliases
pub mod server {
    use {Request, Response};

    use futures::{Future, Stream, Poll};
    use {h2, http};
    use tower::Service;

    /// A specialization of tower::Service.
    ///
    /// Existing tower::Service implementations with the correct form will
    /// automatically implement `GrpcService`.
    ///
    /// TODO: Rename to StreamingService?
    pub trait GrpcService: Clone {
        /// Protobuf request message type
        type Request;

        /// Stream of inbound request messages
        type RequestStream: Stream<Item = Self::Request, Error = ::Error>;

        /// Protobuf response message type
        type Response;

        /// Stream of outbound response messages
        type ResponseStream: Stream<Item = Self::Response, Error = ::Error>;

        /// Response future
        type Future: Future<Item = ::Response<Self::ResponseStream>, Error = ::Error>;

        /// Returns `Ready` when the service can accept a request
        fn poll_ready(&mut self) -> Poll<(), ::Error>;

        /// Call the service
        fn call(&mut self, request: Request<Self::RequestStream>) -> Self::Future;
    }

    impl<T, S1, S2> GrpcService for T
    where T: Service<Request = Request<S1>,
                    Response = Response<S2>,
                       Error = ::Error> + Clone,
          S1: Stream<Error = ::Error>,
          S2: Stream<Error = ::Error>,
    {
        type Request = S1::Item;
        type RequestStream = S1;
        type Response = S2::Item;
        type ResponseStream = S2;
        type Future = T::Future;

        fn poll_ready(&mut self) -> Poll<(), ::Error> {
            Service::poll_ready(self)
        }

        fn call(&mut self, request: T::Request) -> Self::Future {
            Service::call(self, request)
        }
    }

    /// A specialization of tower::Service.
    ///
    /// Existing tower::Service implementations with the correct form will
    /// automatically implement `UnaryService`.
    pub trait UnaryService: Clone {
        /// Protobuf request message type
        type Request;

        /// Protobuf response message type
        type Response;

        /// Response future
        type Future: Future<Item = ::Response<Self::Response>, Error = ::Error>;

        /// Returns `Ready` when the service can accept a request
        fn poll_ready(&mut self) -> Poll<(), ::Error>;

        /// Call the service
        fn call(&mut self, request: Request<Self::Request>) -> Self::Future;
    }

    impl<T, M1, M2> UnaryService for T
    where T: Service<Request = Request<M1>,
                    Response = Response<M2>,
                       Error = ::Error> + Clone,
    {
        type Request = M1;
        type Response = M2;
        type Future = T::Future;

        fn poll_ready(&mut self) -> Poll<(), ::Error> {
            Service::poll_ready(self)
        }

        fn call(&mut self, request: T::Request) -> Self::Future {
            Service::call(self, request)
        }
    }

    /// A specialization of tower::Service.
    ///
    /// Existing tower::Service implementations with the correct form will
    /// automatically implement `UnaryService`.
    pub trait ClientStreamingService: Clone {
        /// Protobuf request message type
        type Request;

        /// Stream of inbound request messages
        type RequestStream: Stream<Item = Self::Request, Error = ::Error>;

        /// Protobuf response message type
        type Response;

        /// Response future
        type Future: Future<Item = ::Response<Self::Response>, Error = ::Error>;

        /// Returns `Ready` when the service can accept a request
        fn poll_ready(&mut self) -> Poll<(), ::Error>;

        /// Call the service
        fn call(&mut self, request: Request<Self::RequestStream>) -> Self::Future;
    }

    impl<T, M, S> ClientStreamingService for T
    where T: Service<Request = Request<S>,
                    Response = Response<M>,
                       Error = ::Error> + Clone,
          S: Stream<Error = ::Error>,
    {
        type Request = S::Item;
        type RequestStream = S;
        type Response = M;
        type Future = T::Future;

        fn poll_ready(&mut self) -> Poll<(), ::Error> {
            Service::poll_ready(self)
        }

        fn call(&mut self, request: T::Request) -> Self::Future {
            Service::call(self, request)
        }
    }

    /// A specialization of tower::Service.
    ///
    /// Existing tower::Service implementations with the correct form will
    /// automatically implement `UnaryService`.
    pub trait ServerStreamingService: Clone {
        /// Protobuf request message type
        type Request;

        /// Protobuf response message type
        type Response;

        /// Stream of outbound response messages
        type ResponseStream: Stream<Item = Self::Response, Error = ::Error>;

        /// Response future
        type Future: Future<Item = ::Response<Self::ResponseStream>, Error = ::Error>;

        /// Returns `Ready` when the service can accept a request
        fn poll_ready(&mut self) -> Poll<(), ::Error>;

        /// Call the service
        fn call(&mut self, request: Request<Self::Request>) -> Self::Future;
    }

    impl<T, M, S> ServerStreamingService for T
    where T: Service<Request = Request<M>,
                    Response = Response<S>,
                       Error = ::Error> + Clone,
          S: Stream<Error = ::Error>,
    {
        type Request = M;
        type Response = S::Item;
        type ResponseStream = S;
        type Future = T::Future;

        fn poll_ready(&mut self) -> Poll<(), ::Error> {
            Service::poll_ready(self)
        }

        fn call(&mut self, request: T::Request) -> Self::Future {
            Service::call(self, request)
        }
    }

    #[derive(Debug)]
    pub struct Grpc<T>
    where T: GrpcService,
    {
        inner: ::server::Grpc<Wrap<T>, ::protobuf::Codec<T::Response, T::Request>>,
    }

    #[derive(Debug)]
    pub struct ResponseFuture<T>
    where T: GrpcService,
    {
        inner: ::server::streaming::ResponseFuture<T::Future, ::protobuf::Encoder<T::Response>>,
    }

    /// A protobuf encoded gRPC request stream
    #[derive(Debug)]
    pub struct Decode<T> {
        inner: ::server::Decode<::protobuf::Decoder<T>>,
    }

    /// A protobuf encoded gRPC response body
    pub struct Encode<T>
    where T: Stream,
    {
        inner: ::server::Encode<T, ::protobuf::Encoder<T::Item>>,
    }

    // ===== impl Grpc =====

    impl<T, U> Grpc<T>
    where T: GrpcService<Request = U, RequestStream = Decode<U>>,
          T::Request: ::prost::Message + Default,
          T::Response: ::prost::Message,
    {
        pub fn new(inner: T) -> Self {
            let inner = ::server::Grpc::new(Wrap(inner), ::protobuf::Codec::new());
            Grpc { inner }
        }
    }

    impl<T, U> Service for Grpc<T>
    where T: GrpcService<Request = U, RequestStream = Decode<U>>,
          T::Request: ::prost::Message + Default,
          T::Response: ::prost::Message,
    {
        type Request = ::http::Request<::tower_h2::RecvBody>;
        type Response = ::http::Response<Encode<T::ResponseStream>>;
        type Error = ::h2::Error;
        type Future = ResponseFuture<T>;

        fn poll_ready(&mut self) -> Poll<(), Self::Error> {
            self.inner.poll_ready()
                .map_err(Into::into)
        }

        fn call(&mut self, request: Self::Request) -> Self::Future {
            let inner = self.inner.call(request);
            ResponseFuture { inner }
        }
    }

    impl<T> Clone for Grpc<T>
    where T: GrpcService + Clone,
    {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Grpc { inner }
        }
    }

    // ===== impl ResponseFuture =====

    impl<T> Future for ResponseFuture<T>
    where T: GrpcService,
          T::Response: ::prost::Message,
    {
        type Item = ::http::Response<Encode<T::ResponseStream>>;
        type Error = ::h2::Error;

        fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
            let response = try_ready!(self.inner.poll());
            let (head, inner) = response.into_parts();
            let body = Encode { inner };
            let response = ::http::Response::from_parts(head, body);
            Ok(response.into())
        }
    }

    // ===== impl Encode =====

    impl<T> ::tower_h2::Body for Encode<T>
    where T: Stream<Error = ::Error>,
          T::Item: ::prost::Message,
    {
        type Data = ::bytes::Bytes;

        fn is_end_stream(&self) -> bool {
            false
        }

        fn poll_data(&mut self) -> Poll<Option<Self::Data>, ::h2::Error> {
            self.inner.poll_data()
        }

        fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
            self.inner.poll_trailers()
        }
    }

    // ===== impl Decode =====

    impl<T> Stream for Decode<T>
    where T: ::prost::Message + Default,
    {
        type Item = T;
        type Error = ::Error;

        fn poll(&mut self) -> Poll<Option<T>, ::Error> {
            self.inner.poll()
        }
    }

    // ===== impl Wrap =====

    #[derive(Debug, Clone)]
    struct Wrap<T>(T);

    impl<T, U> Service for Wrap<T>
    where T: GrpcService<Request = U, RequestStream = Decode<U>>,
          T::Request: ::prost::Message + Default,
          T::Response: ::prost::Message,
    {
        type Request = Request<::server::Decode<::protobuf::Decoder<T::Request>>>;
        type Response = Response<T::ResponseStream>;
        type Error = ::Error;
        type Future = T::Future;

        fn poll_ready(&mut self) -> Poll<(), ::Error> {
            self.0.poll_ready()
        }

        fn call(&mut self, request: Self::Request) -> Self::Future {
            let request = request.map(|inner| Decode { inner });
            self.0.call(request)
        }
    }
}

// ===== impl Codec =====

impl<T, U> Codec<T, U>
where T: Message,
      U: Message + Default,
{
    /// Create a new protobuf codec
    pub fn new() -> Self {
        Codec(PhantomData)
    }
}

impl<T, U> ::server::Codec for Codec<T, U>
where T: Message,
      U: Message + Default,
{
    /// Protocol buffer gRPC content type
    const CONTENT_TYPE: &'static str = "application/grpc+proto";

    type Encode = T;
    type Encoder = Encoder<T>;
    type Decode = U;
    type Decoder = Decoder<U>;

    fn encoder(&mut self) -> Self::Encoder {
        Encoder(PhantomData)
    }

    fn decoder(&mut self) -> Self::Decoder {
        Decoder(PhantomData)
    }
}

impl<T, U> Clone for Codec<T, U> {
    fn clone(&self) -> Self {
        Codec(PhantomData)
    }
}

// ===== impl Encoder =====

impl<T> ::server::Encoder for Encoder<T>
where T: Message,
{
    type Item = T;

    fn encode(&mut self, item: T, buf: &mut EncodeBuf) -> Result<(), ::Error> {
        let len = item.encoded_len();

        if buf.remaining_mut() < len {
            buf.reserve(len);
        }

        item.encode(buf)
            .map_err(|_| unreachable!("Message only errors if not enough space"))
    }
}

impl<T> Clone for Encoder<T> {
    fn clone(&self) -> Self {
        Encoder(PhantomData)
    }
}

// ===== impl Decoder =====

impl<T> ::server::Decoder for Decoder<T>
where T: Message + Default,
{
    type Item = T;

    fn decode(&mut self, buf: &mut DecodeBuf) -> Result<T, ::Error> {
        Message::decode(buf)
            .map_err(|_| unimplemented!())
    }
}

impl<T> Clone for Decoder<T> {
    fn clone(&self) -> Self {
        Decoder(PhantomData)
    }
}
