
use futures::Poll;
use futures_watch::Watch;
use http;
use tower::Service;

use std::fmt::{self, Write};
use std::sync::Arc;

use ctx;

/// Middleware that adds an extension containing an optional set of metric
/// labels to requests.
#[derive(Clone, Debug)]
pub struct Labeled<T> {
    metric_labels: Option<Watch<Option<DstLabels>>>,
    inner: T,
}

#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub struct RequestLabels {

    /// Was the request in the inbound or outbound direction?
    direction: Direction,

    // Additional labels identifying the destination service of an outbound
    // request, provided by the Conduit control plane's service discovery.
    outbound_labels: Option<DstLabels>,

    /// The value of the `:authority` (HTTP/2) or `Host` (HTTP/1.1) header of
    /// the request.
    authority: String,
}

#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub struct ResponseLabels {

    request_labels: RequestLabels,

    /// The HTTP status code of the response.
    status_code: u16,

    /// The value of the grpc-status trailer. Only applicable to response
    /// metrics for gRPC responses.
    grpc_status_code: Option<u32>,

    /// Was the response a success or failure?
    classification: Classification,
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
enum Classification {
    Success,
    Failure,
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
enum Direction {
    Inbound,
    Outbound,
}

#[derive(Clone, Debug, Hash, Eq, PartialEq)]
pub struct DstLabels(Arc<str>);

// ===== impl Labeled =====

impl<T> Labeled<T> {

    /// Wrap `inner` with a `Watch` on dyanmically updated labels.
    pub fn new(inner: T, watch: Watch<Option<DstLabels>>) -> Self {
        Self {
            metric_labels: Some(watch),
            inner,
        }
    }

    /// Wrap `inner` with no `metric_labels`.
    pub fn none(inner: T) -> Self {
        Self { metric_labels: None, inner }
    }
}

impl<T, A> Service for Labeled<T>
where
    T: Service<Request=http::Request<A>>,
{
    type Request = T::Request;
    type Response= T::Response;
    type Error = T::Error;
    type Future = T::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let mut req = req;
        if let Some(labels) = self.metric_labels.as_ref()
            .and_then(|labels| (*labels.borrow()).as_ref().cloned())
        {
            req.extensions_mut().insert(labels);
        }
        self.inner.call(req)
    }

}

// ===== impl RequestLabels =====

impl<'a> RequestLabels {
    pub fn new(req: &ctx::http::Request) -> Self {
        let direction = Direction::from_context(req.server.proxy.as_ref());

        let outbound_labels = req.dst_labels.as_ref().cloned();

        let authority = req.uri
            .authority_part()
            .map(http::uri::Authority::to_string)
            .unwrap_or_else(String::new);

        RequestLabels {
            direction,
            outbound_labels,
            authority,
        }
    }
}

impl fmt::Display for RequestLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "authority=\"{}\",{}", self.authority, self.direction)?;

        if let Some(ref outbound) = self.outbound_labels {
            // leading comma added between the direction label and the
            // destination labels, if there are destination labels.
            write!(f, ",{}", outbound)?;
        }

        Ok(())
    }
}

// ===== impl ResponseLabels =====

impl ResponseLabels {

    pub fn new(rsp: &ctx::http::Response, grpc_status_code: Option<u32>) -> Self {
        let request_labels = RequestLabels::new(&rsp.request);
        let classification = Classification::classify(rsp, grpc_status_code);
        ResponseLabels {
            request_labels,
            status_code: rsp.status.as_u16(),
            grpc_status_code,
            classification,
        }
    }

    /// Called when the response stream has failed.
    pub fn fail(rsp: &ctx::http::Response) -> Self {
        let request_labels = RequestLabels::new(&rsp.request);
        ResponseLabels {
            request_labels,
            // TODO: is it correct to always treat this as 500?
            // Alternatively, the status_code field could be made optional...
            status_code: 500,
            grpc_status_code: None,
            classification: Classification::Failure,
        }
    }
}

impl fmt::Display for ResponseLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{},{},status_code=\"{}\"",
            self.request_labels,
            self.classification,
            self.status_code
        )?;

        if let Some(ref status) = self.grpc_status_code {
            // leading comma added between the status code label and the
            // gRPC status code labels, if there is a gRPC status code.
            write!(f, ",grpc_status_code=\"{}\"", status)?;
        }

        Ok(())
    }
}

// ===== impl Classification =====

impl Classification {

    fn grpc_status(code: u32) -> Self {
        if code == 0 {
            // XXX: are gRPC status codes indicating client side errors
            //      "successes" or "failures?
            Classification::Success
        } else {
            Classification::Failure
        }
    }

    fn http_status(status: &http::StatusCode) -> Self {
        if status.is_server_error() {
            Classification::Failure
        } else {
            Classification::Success
        }
    }

    fn classify(rsp: &ctx::http::Response, grpc_status: Option<u32>) -> Self {
        grpc_status.map(Classification::grpc_status)
            .unwrap_or_else(|| Classification::http_status(&rsp.status))
    }

}

impl fmt::Display for Classification {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            &Classification::Success => f.pad("classification=\"success\""),
            &Classification::Failure => f.pad("classification=\"failure\""),
        }
    }
}

// ===== impl Direction =====

impl Direction {
    fn from_context(context: &ctx::Proxy) -> Self {
        match context {
            &ctx::Proxy::Inbound(_) => Direction::Inbound,
            &ctx::Proxy::Outbound(_) => Direction::Outbound,
        }
    }
}

impl fmt::Display for Direction {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            &Direction::Inbound => f.pad("direction=\"inbound\""),
            &Direction::Outbound => f.pad("direction=\"outbound\""),
        }
    }
}


// ===== impl DstLabels ====

impl DstLabels {
    pub fn new<I, S>(labels: I) -> Option<Self>
    where
        I: IntoIterator<Item=(S, S)>,
        S: fmt::Display,
    {
        let mut labels = labels.into_iter();

        if let Some((k, v)) = labels.next() {
            // Format the first label pair without a leading comma, since we
            // don't know where it is in the output labels at this point.
            let mut s = format!("dst_{}=\"{}\"", k, v);

            // Format subsequent label pairs with leading commas, since
            // we know that we already formatted the first label pair.
            for (k, v) in labels {
                write!(s, ",dst_{}=\"{}\"", k, v)
                    .expect("writing to string should not fail");
            }

            Some(DstLabels(Arc::from(s)))
        } else {
            // The iterator is empty; return None
            None
        }
    }
}

impl fmt::Display for DstLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

#[cfg(test)]
mod test {
    use super::*;

    use std::sync::atomic::{AtomicUsize, Ordering};

    use futures::{Async, Poll, Future};
    use futures::future::{self, FutureResult};
    use http;
    use tower::Service;


    struct MockInnerService<'a> {
        expected_labels: &'a [Option<&'a str>],
        num_requests: AtomicUsize,
    }

    impl<'a> MockInnerService<'a> {
        fn new(expected_labels: &'a [Option<&'a str>]) -> Self {
            MockInnerService {
                expected_labels,
                num_requests: AtomicUsize::new(0),
            }
        }
    }

    impl<'a> Service for MockInnerService<'a> {
        type Request = http::Request<()>;
        type Response = ();
        type Error = ();
        type Future = FutureResult<Self::Response, Self::Error>;

        fn poll_ready(&mut self) -> Poll<(), Self::Error> {
            Ok(Async::Ready(()))
        }

        fn call(&mut self, req: Self::Request) -> Self::Future {
            let n = self.num_requests.fetch_add(1, Ordering::SeqCst);
            let n = if n > self.expected_labels.len() {
                self.expected_labels.len()
            } else {
                n
            };
            let req_labels = req.extensions()
                .get::<DstLabels>()
                .map(|DstLabels(ref inner)| inner.as_ref());
            assert_eq!(req_labels, self.expected_labels[n]);
            future::ok(())
        }
    }

    #[test]
    fn no_labels() {
        let expected_labels = [None];
        let inner = MockInnerService::new(&expected_labels[..]);
        let mut labeled = Labeled {
            metric_labels: None,
            inner
        };
        // If the request has been labeled, the assertion in
        // `MockInnerService` will panic.
        labeled.call(http::Request::new(())).wait().unwrap();
    }

    #[test]
    fn one_label() {
        let expected_labels = [Some("dst_foo=\"bar\"")];
        let inner = MockInnerService::new(&expected_labels[..]);
        let (watch, _) =
            Watch::new(DstLabels::new(vec![("foo", "bar")]));
        let mut labeled = Labeled {
            metric_labels: Some(watch),
            inner
        };
        // If the request has not been labeled with `dst_foo="bar"`,
        // the assertion in `MockInnerService` will panic.
        labeled.call(http::Request::new(())).wait().unwrap();
    }

    #[test]
    fn label_updates() {
        let expected_labels = [
            Some("dst_foo=\"bar\""),
            Some("dst_foo=\"baz\""),
            Some("dst_foo=\"baz\",dst_quux=\"quuux\""),
        ];
        let inner = MockInnerService::new(&expected_labels[..]);
        let (watch, mut store) =
            Watch::new(DstLabels::new(vec![("foo", "bar")]));
        let mut labeled = Labeled {
            metric_labels: Some(watch),
            inner
        };
        labeled.call(http::Request::new(())).wait().expect("first call");

        store.store(DstLabels::new(vec![("foo", "baz")]))
            .expect("store (\"foo\", \"baz\")");
        labeled.call(http::Request::new(())).wait().expect("second call");

        store.store(DstLabels::new(vec![
            ("foo", "baz"),
            ("quux", "quuux")
        ]))
            .expect("store (\"foo\", \"baz\"), (\"quux\", \"quuux\")");
        labeled.call(http::Request::new(())).wait().expect("third call");
    }

}
