
use http;
use std::fmt;

use control::discovery;
use ctx;

#[derive(Clone, Debug, Default, Eq, PartialEq, Hash)]
pub struct RequestLabels {
    // Additional labels identifying the destination service of an outbound
    // request, provided by the Conduit control plane's service discovery.
    outbound_labels: Option<discovery::DstLabels>,

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


// ===== impl RequestLabels =====

impl<'a> RequestLabels {
    fn new(req: &ctx::http::Request) -> Self {
        let outbound_labels = req.dst_labels.as_ref().cloned();

        let authority = req.uri
            .authority_part()
            .map(http::uri::Authority::to_string)
            .unwrap_or_else(String::new);

        RequestLabels {
            outbound_labels,
            authority,
        }
    }
}

impl fmt::Display for RequestLabels {

    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "authority=\"{}\",", self.authority)?;
        if let Some(ref outbound) = self.outbound_labels {
            write!(f, "direction=\"outbound\"{}", outbound)?;
        } else {
            write!(f, "direction=\"inbound\"")?;
        }

        Ok(())
    }

}



// ===== impl ResponseLabels =====

impl ResponseLabels {

    fn new(rsp: &ctx::http::Response, grpc_status_code: Option<u32>) -> Self {
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
    fn fail(rsp: &ctx::http::Response) -> Self {
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
