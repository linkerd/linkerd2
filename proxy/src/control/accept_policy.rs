use std;
use std::collections::{HashSet, VecDeque};
use std::collections::hash_map::{Entry, HashMap};
use std::net::SocketAddr;
use std::fmt;
use std::mem;

use futures::{Async, Future, Poll, Stream};
use futures_watch::Watch;
use tower::Service;
use tower_h2::{HttpService, BoxBody, RecvBody};
use tower_grpc as grpc;

use conduit_proxy_controller_grpc::accept_policy;

/// A handle to start watching a destination for address changes.
#[derive(Clone, Debug)]
pub struct AcceptPolicy {
}


impl AcceptPolicy
{
    pub fn new() -> Self {
        Self {}
    }

    pub fn poll_rpc<S>(&mut self, client: &mut S)
    where
        S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
    {
    }
}
