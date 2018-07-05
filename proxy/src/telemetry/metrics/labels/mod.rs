use std::{
    collections::HashMap,
    fmt::{self, Write},
    hash,
    net::SocketAddr,
    path::PathBuf,
    sync::Arc,
};

use http;

use ctx;
use connection;
use telemetry::event;
use transport::tls;

macro_rules! mk_err_enum {
    { $(#[$m:meta])* enum $name:ident from $from_ty:ty {
         $( $from:pat => $reason:ident ),+
     } } => {
        $(#[$m])*
        pub enum $name {
            $( $reason ),+
        }

        impl fmt::Display for $name {
             fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
                // use super::$name::*;
                 match self {
                    $(
                        $name::$reason => f.pad(stringify!($reason))
                    ),+
                }
             }
        }

        impl<'a> From<$from_ty> for $name {
            fn from(err: $from_ty) -> Self {
                match err {
                    $(
                        $from => $name::$reason
                    ),+
                }
            }
        }
    }
}

mod errno;
use self::errno::Errno;

#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub struct RequestLabels {

    /// Was the request in the inbound or outbound direction?
    direction: Direction,

    // Additional labels identifying the destination service of an outbound
    // request, provided by the Conduit control plane's service discovery.
    outbound_labels: Option<DstLabels>,

    /// The value of the `:authority` (HTTP/2) or `Host` (HTTP/1.1) header of
    /// the request.
    authority: Option<http::uri::Authority>,

    /// Whether or not the request was made over TLS.
    tls_status: TlsStatus,
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

/// Labels describing a TCP connection
#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
pub struct TransportLabels {
    /// Was the transport opened in the inbound or outbound direction?
    direction: Direction,

    peer: Peer,

    /// Was the transport secured with TLS?
    tls_status: TlsStatus,
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
pub enum Peer { Src, Dst }

/// Labels describing the end of a TCP connection
#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
pub struct TransportCloseLabels {
    /// Labels describing the TCP connection that closed.
    pub(super) transport: TransportLabels,

    /// Was the transport closed successfully?
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

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DstLabels {
    formatted: Arc<str>,
    original: Arc<HashMap<String, String>>,
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
pub struct TlsStatus(ctx::transport::TlsStatus);

/// Labels describing a TLS handshake failure.
#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
pub enum HandshakeFailLabels {
    Proxy {
        /// Labels describing the TCP connection that closed.
        transport: TransportLabels,
        reason: HandshakeFailReason,
    },
    Control {
        peer: Peer,
        remote_addr: SocketAddr,
        reason: HandshakeFailReason,
    },

}


#[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
pub enum HandshakeFailReason {
    Io(Option<Errno>),
    Tls(TlsError),
}

mk_err_enum! {
    /// Translates `rustls::TLSError` variants into a `Copy` type suitable for
    /// labels.
    #[allow(non_camel_case_types)]
    #[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
    enum TlsError from &'a tls::Error {
        tls::Error::InappropriateMessage {..} => INAPPROPRIATE_MESSAGE,
        tls::Error::InappropriateHandshakeMessage {..} => INAPPROPRIATE_HANDSHAKE_MESSAGE,
        tls::Error::CorruptMessage => CORRUPT_MESSAGE,
        tls::Error::CorruptMessagePayload(_) => CORRUPT_MESSAGE_PAYLOAD,
        tls::Error::NoCertificatesPresented => NO_CERTIFICATES_PRESENTED,
        tls::Error::DecryptError => DECRYPT_ERROR,
        tls::Error::PeerIncompatibleError(_) => PEER_INCOMPATIBLE,
        tls::Error::PeerMisbehavedError(_) => PEER_MISBEHAVED,
        tls::Error::AlertReceived(_) => ALERT_RECEIVED,
        tls::Error::WebPKIError(tls::WebPkiError::BadDER) => BAD_DER,
        tls::Error::WebPKIError(tls::WebPkiError::BadDERTime) => BAD_DER_TIME,
        tls::Error::WebPKIError(tls::WebPkiError::CAUsedAsEndEntity) => CA_USED_AS_END_ENTITY,
        tls::Error::WebPKIError(tls::WebPkiError::CertExpired) => CERT_EXPIRED,
        tls::Error::WebPKIError(tls::WebPkiError::CertNotValidForName) =>
            CERT_NOT_VALID_FOR_NAME,
        tls::Error::WebPKIError(tls::WebPkiError::CertNotValidYet) =>
            CERT_NOT_VALID_YET,
        tls::Error::WebPKIError(tls::WebPkiError::EndEntityUsedAsCA) =>
            END_ENTITY_USED_AS_CA,
        tls::Error::WebPKIError(tls::WebPkiError::ExtensionValueInvalid) =>
            EXTENSION_VALUE_INVALID,
        tls::Error::WebPKIError(tls::WebPkiError::InvalidCertValidity) =>
            INVALID_CERT_VALIDITY,
        tls::Error::WebPKIError(tls::WebPkiError::InvalidSignatureForPublicKey) =>
            INVALID_SIGNATURE_FOR_PUBLIC_KEY,
        tls::Error::WebPKIError(tls::WebPkiError::NameConstraintViolation) =>
            NAME_CONSTRAINT_VIOLATION,
        tls::Error::WebPKIError(tls::WebPkiError::PathLenConstraintViolated) =>
            PATH_LEN_CONSTRAINT_VIOLATED,
        tls::Error::WebPKIError(tls::WebPkiError::SignatureAlgorithmMismatch) =>
            SIGNATURE_ALGORITHM_MISMATCH,
        tls::Error::WebPKIError(tls::WebPkiError::RequiredEKUNotFound) =>
            REQUESTED_EKU_NOT_FOUND,
        tls::Error::WebPKIError(tls::WebPkiError::UnknownIssuer) =>
            UNKNOWN_ISSUER,
        tls::Error::WebPKIError(tls::WebPkiError::UnsupportedCertVersion) =>
            UNSUPPORTED_CERT_VERSION,
        tls::Error::WebPKIError(tls::WebPkiError::UnsupportedCriticalExtension) =>
            UNSUPPORTED_CRITICAL_EXTENSION,
        tls::Error::WebPKIError(tls::WebPkiError::UnsupportedSignatureAlgorithmForPublicKey) =>
            UNSUPPORTED_SIGNATURE_ALGORITHM_FOR_PUBLIC_KEY,
        tls::Error::WebPKIError(tls::WebPkiError::UnsupportedSignatureAlgorithm) =>
            UNSUPPORTED_SIGNATURE_ALGORITHM,
        tls::Error::InvalidSCT(_) => INVALID_SCT,
        tls::Error::General(_) => UNKNOWN,
        tls::Error::FailedToGetCurrentTime => FAILED_TO_GET_CURRENT_TIME,
        tls::Error::InvalidDNSName(_) => INVALID_DNS_NAME,
        tls::Error::HandshakeNotComplete => HANDSHAKE_NOT_COMPLETE,
        tls::Error::PeerSentOversizedRecord => PEER_SENT_OVERSIZED_RECORD
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub enum TlsConfigLabels {
    Reloaded,
    InvalidTrustAnchors,
    InvalidPrivateKey,
    InvalidEndEntityCert,
    Io { path: PathBuf, errno: Option<Errno>, },
}

// ===== impl RequestLabels =====

impl RequestLabels {
    pub fn new(req: &ctx::http::Request) -> Self {
        let direction = Direction::from_context(req.server.proxy.as_ref());

        let outbound_labels = req.dst_labels().cloned();

        let authority = req.uri
            .authority_part()
            .cloned();

        RequestLabels {
            direction,
            outbound_labels,
            authority,
            tls_status: TlsStatus(req.tls_status()),
        }
    }

    #[cfg(test)]
    pub fn tls_status(&self) -> TlsStatus {
        self.tls_status
    }
}

impl fmt::Display for RequestLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self.authority {
            Some(ref authority) =>
                write!(f, "authority=\"{}\",{}", authority, self.direction),
            None =>
                write!(f, "authority=\"\",{}", self.direction),
        }?;

        if let Some(ref outbound) = self.outbound_labels {
            // leading comma added between the direction label and the
            // destination labels, if there are destination labels.
            write!(f, ",{}", outbound)?;
        }

        write!(f, ",{}", self.tls_status)?;

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

    #[cfg(test)]
    pub fn tls_status(&self) -> TlsStatus {
        self.request_labels.tls_status
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

    fn transport_close(close: &event::TransportClose) -> Self {
        if close.clean {
            Classification::Success
        } else {
            Classification::Failure
        }
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
            let mut original = HashMap::new();

            // Format the first label pair without a leading comma, since we
            // don't know where it is in the output labels at this point.
            let mut s = format!("dst_{}=\"{}\"", k, v);
            original.insert(format!("{}", k), format!("{}", v));

            // Format subsequent label pairs with leading commas, since
            // we know that we already formatted the first label pair.
            for (k, v) in labels {
                write!(s, ",dst_{}=\"{}\"", k, v)
                    .expect("writing to string should not fail");
                original.insert(format!("{}", k), format!("{}", v));
            }

            Some(DstLabels {
                formatted: Arc::from(s),
                original: Arc::new(original),
            })
        } else {
            // The iterator is empty; return None
            None
        }
    }

    pub fn as_map(&self) -> &HashMap<String, String> {
        &self.original
    }

    pub fn as_str(&self) -> &str {
        &self.formatted
    }
}

// Simply hash the formatted string and no other fields on `DstLabels`.
impl hash::Hash for DstLabels {
    fn hash<H: hash::Hasher>(&self, state: &mut H) {
        self.formatted.hash(state)
    }
}

impl fmt::Display for DstLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        self.formatted.fmt(f)
    }
}


// ===== impl TransportLabels =====

impl TransportLabels {
    pub fn new(ctx: &ctx::transport::Ctx) -> Self {
        TransportLabels {
            direction: Direction::from_context(&ctx.proxy()),
            peer: match *ctx {
                ctx::transport::Ctx::Server(_) => Peer::Src,
                ctx::transport::Ctx::Client(_) => Peer::Dst,
            },
            tls_status: TlsStatus(ctx.tls_status()),
        }
    }

    #[cfg(test)]
    pub fn tls_status(&self) -> TlsStatus {
        self.tls_status
    }
}

impl fmt::Display for TransportLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{},{},{}", self.direction, self.peer, self.tls_status)
    }
}

impl fmt::Display for Peer {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            Peer::Src => f.pad("peer=\"src\""),
            Peer::Dst => f.pad("peer=\"dst\""),
        }
    }
}

// ===== impl TransportCloseLabels =====

impl TransportCloseLabels {
    pub fn new(ctx: &ctx::transport::Ctx,
               close: &event::TransportClose)
               -> Self {
        TransportCloseLabels {
            transport: TransportLabels::new(ctx),
            classification: Classification::transport_close(close),
        }
    }

    #[cfg(test)]
    pub fn tls_status(&self) -> TlsStatus  {
        self.transport.tls_status()
    }
}

impl fmt::Display for TransportCloseLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{},{}", self.transport, self.classification)
    }
}

// ===== impl TlsStatus =====

impl fmt::Display for TlsStatus {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "tls=\"{}\"", self.0)
    }
}

impl From<ctx::transport::TlsStatus> for TlsStatus {
    fn from(tls: ctx::transport::TlsStatus) -> Self {
        TlsStatus(tls)
    }
}


impl Into<ctx::transport::TlsStatus> for TlsStatus {
    fn into(self) -> ctx::transport::TlsStatus {
        self.0
    }
}

impl HandshakeFailLabels {
    pub fn proxy(ctx: &ctx::transport::Ctx,
                 err: &connection::HandshakeError)
               -> Self {
        HandshakeFailLabels::Proxy {
            transport: TransportLabels::new(ctx),
            reason: err.into(),
        }
    }

    pub fn control(ctx: &event::ControlConnection,
                   err: &connection::HandshakeError)
                   -> Self {
        let (peer, remote_addr) = match ctx {
            event::ControlConnection::Accept { remote_addr, .. } =>
                (Peer::Dst, *remote_addr),
            event::ControlConnection::Connect { remote_addr } =>
                (Peer::Src, *remote_addr),
        };
        HandshakeFailLabels::Control {
            peer,
            remote_addr,
            reason: err.into(),
        }
    }
}

impl fmt::Display for HandshakeFailLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            HandshakeFailLabels::Proxy { transport, reason } =>
                write!(f, "{},{}", transport, reason),
            HandshakeFailLabels::Control { peer, remote_addr, reason } =>
                write!(f, "{},remote=\"{}\",{}",peer, remote_addr, reason),
        }
    }
}

impl fmt::Display for HandshakeFailReason {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            HandshakeFailReason::Tls(ref reason) =>
                write!(f, "reason=\"tls_error\",tls_error=\"{}\"", reason),
            HandshakeFailReason::Io(Some(ref errno)) =>
                write!(f, "reason=\"io_error\",errno=\"{}\"", errno),
            HandshakeFailReason::Io(None) =>
                f.pad("reason=\"io_error\",errno=\"unknown\""),
        }
    }
}

impl<'a> From<&'a connection::HandshakeError> for HandshakeFailReason {
    fn from(err: &'a connection::HandshakeError) -> Self {
        match err {
            connection::HandshakeError::Io { ref errno } =>
                HandshakeFailReason::Io(errno.map(Errno::from)),
            connection::HandshakeError::Tls(ref tls_err) =>
                HandshakeFailReason::Tls(tls_err.into()),
        }
    }
}

// ===== impl TlsConfigLabels =====

impl TlsConfigLabels {
    pub fn success() -> Self {
        TlsConfigLabels::Reloaded
    }
}

impl From<tls::ConfigError> for TlsConfigLabels {
    fn from(err: tls::ConfigError) -> Self {
        match err {
            tls::ConfigError::Io(path, error_code) =>
                TlsConfigLabels::Io { path, errno: error_code.map(Errno::from) },
            tls::ConfigError::FailedToParseTrustAnchors(_) =>
                TlsConfigLabels::InvalidTrustAnchors,
            tls::ConfigError::EndEntityCertIsNotValid(_) =>
                TlsConfigLabels::InvalidEndEntityCert,
            tls::ConfigError::InvalidPrivateKey =>
                TlsConfigLabels::InvalidPrivateKey,
        }
    }
}


impl fmt::Display for TlsConfigLabels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            TlsConfigLabels::Reloaded =>
                f.pad("status=\"reloaded\""),
            TlsConfigLabels::Io { ref path, errno: Some(errno) } =>
                write!(f,
                    "status=\"io_error\",path=\"{}\",errno=\"{}\"",
                    path.display(), errno
                ),
            TlsConfigLabels::Io { ref path, errno: None } =>
                write!(f,
                    "status=\"io_error\",path=\"{}\",errno=\"UNKNOWN\"",
                    path.display(),
                ),
            TlsConfigLabels::InvalidPrivateKey =>
                f.pad("status=\"invalid_private_key\""),
            TlsConfigLabels::InvalidEndEntityCert =>
                f.pad("status=\"invalid_end_entity_cert\""),
            TlsConfigLabels::InvalidTrustAnchors =>
                f.pad("status=\"invalid_trust_anchors\""),
        }
    }
}
