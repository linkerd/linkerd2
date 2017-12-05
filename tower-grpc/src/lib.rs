#![deny(warnings)]
//#![deny(missing_docs)]
//#![deny(missing_debug_implementations)]

extern crate bytes;
#[macro_use] extern crate futures;
extern crate http;
extern crate h2;
#[macro_use] extern crate log;
extern crate tower;
extern crate tower_h2;

#[cfg(feature = "protobuf")]
extern crate prost;

pub mod client;
pub mod server;

#[cfg(feature = "protobuf")]
pub mod protobuf;

mod error;
mod request;
mod response;
mod status;

pub use self::client::Client;
pub use self::error::Error;
pub use self::status::{Code, Status};
pub use self::request::Request;
pub use self::response::Response;

/// Type re-exports used by generated code
pub mod codegen {
    /// Type re-exports used by generated server code
    pub mod server {
        /// Re-export types from this crate
        pub mod grpc {
            pub use ::{Request, Response, Error, Status};
            pub use ::server::{
                unary,
                Unary,
                ClientStreaming,
                ServerStreaming,
                NotImplemented,
            };
            pub use ::protobuf::server::{
                Grpc,
                GrpcService,
                UnaryService,
                ClientStreamingService,
                ServerStreamingService,
                Encode,
                Decode,
            };
        }

        /// Re-export types from the `bytes` crate.
        pub mod bytes {
            pub use ::bytes::Bytes;
        }

        /// Re-export types from the `future` crate.
        pub mod futures {
            pub use ::futures::{Future, Poll, Async};
            pub use ::futures::future::{FutureResult, ok};
        }

        /// Re-exported types from the `http` crate.
        pub mod http {
            pub use ::http::{Request, Response, HeaderMap};
        }

        /// Re-exported types from the `h2` crate.
        pub mod h2 {
            pub use ::h2::Error;
        }

        /// Re-export types from the `tower_h2` crate
        pub mod tower_h2 {
            pub use ::tower_h2::{Body, RecvBody};
        }

        /// Re-exported types from the `tower` crate.
        pub mod tower {
            pub use ::tower::{Service, NewService};
        }
    }
}
