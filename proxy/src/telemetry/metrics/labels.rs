use std::{
    collections::HashMap,
    fmt::{self, Write},
    hash,
    path::PathBuf,
    sync::Arc,
};

use http;

use ctx;
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

#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub enum TlsConfigLabels {
    Reloaded,
    InvalidTrustAnchors,
    InvalidPrivateKey,
    InvalidEndEntityCert,
    Io { path: PathBuf, errno: Option<Errno>, },
}

mk_err_enum! {
    /// Taken from `errno.h`.
    #[allow(non_camel_case_types)]
    #[derive(Copy, Clone, Debug, Eq, PartialEq, Hash)]
    enum Errno from i32 {
        1 => EPERM,
        2 => ENOENT,
        3 => ESRCH,
        4 => EINTR,     // Interrupted system call
        5 => EIO,       // I/O error
        6 => ENXIO,     // No such device or address
        7 => E2BIG,     // Argument list too long
        8 => ENOEXEC,   // Exec format error
        9 => EBADF,     // Bad file number
        10 => ECHILD,    // No child processes
        11 => EAGAIN,    // Try again
        12 => ENOMEM,    // Out of memory
        13 => EACCES,    // Permission denied
        14 => EFAULT,    // Bad address
        15 => ENOTBLK,   // Block device required
        16 => EBUSY,     // Device or resource busy
        17 => EEXIST,    // File exists
        18 => EXDEV,     // Cross-device link
        19 => ENODEV,    // No such device
        20 => ENOTDIR,   // Not a directory
        21 => EISDIR,    // Is a directory
        22 => EINVAL,    // Invalid argument
        23 => ENFILE,    // File table overflow
        24 => EMFILE,    // Too many open files
        25 => ENOTTY,    // Not a typewriter
        26 => ETXTBSY,   // Text file busy
        27 => EFBIG,     // File too large
        28 => ENOSPC,    // No space left on device
        29 => ESPIPE,    // Illegal seek
        30 => EROFS,     // Read-only file system
        31 => EMLINK,    // Too many links
        32 => EPIPE,     // Broken pipe
        33 => EDOM,      // Math argument out of domain of func
        34 => ERANGE,    // Math result not representable
        35  => EDEADLK        ,  // Resource deadlock would occur
        36  => ENAMETOOLONG   ,  // File name too long
        37  => ENOLCK         ,  // No record locks available
        38  => ENOSYS         ,  // Function not implemented
        39  => ENOTEMPTY      ,  // Directory not empty
        40  => ELOOP          ,  // Too many symbolic links encountered
        42  => ENOMSG         ,  // No message of desired type
        43  => EIDRM          ,  // Identifier removed
        44  => ECHRNG         ,  // Channel number out of range
        45  => EL2NSYNC       ,  // Level 2 not synchronized
        46  => EL3HLT         ,  // Level 3 halted
        47  => EL3RST         ,  // Level 3 reset
        48  => ELNRNG         ,  // Link number out of range
        49  => EUNATCH        ,  // Protocol driver not attached
        50  => ENOCSI         ,  // No CSI structure available
        51  => EL2HLT         ,  // Level 2 halted
        52  => EBADE          ,  // Invalid exchange
        53  => EBADR          ,  // Invalid request descriptor
        54  => EXFULL         ,  // Exchange full
        55  => ENOANO         ,  // No anode
        56  => EBADRQC        ,  // Invalid request code
        57  => EBADSLT        ,  // Invalid slot
        59  => EBFONT         ,  // Bad font file format
        60  => ENOSTR         ,  // Device not a stream
        61  => ENODATA        ,  // No data available
        62  => ETIME          ,  // Timer expired
        63  => ENOSR          ,  // Out of streams resources
        64  => ENONET         ,  // Machine is not on the network
        65  => ENOPKG         ,  // Package not installed
        66  => EREMOTE        ,  // Object is remote
        67  => ENOLINK        ,  // Link has been severed
        68  => EADV           ,  // Advertise error
        69  => ESRMNT         ,  // Srmount error
        70  => ECOMM          ,  // Communication error on send
        71  => EPROTO         ,  // Protocol error
        72  => EMULTIHOP      ,  // Multihop attempted
        73  => EDOTDOT        ,  // RFS specific error
        74  => EBADMSG        ,  // Not a data message
        75  => EOVERFLOW      ,  // Value too large for defined data type
        76  => ENOTUNIQ       ,  // Name not unique on network
        77  => EBADFD         ,  // File descriptor in bad state
        78  => EREMCHG        ,  // Remote address changed
        79  => ELIBACC        ,  // Can not access a needed shared library
        80  => ELIBBAD        ,  // Accessing a corrupted shared library
        81  => ELIBSCN        ,  // .lib section in a.out corrupted
        82  => ELIBMAX        ,  // Attempting to link in too many shared libraries
        83  => ELIBEXEC       ,  // Cannot exec a shared library directly
        84  => EILSEQ         ,  // Illegal byte sequence
        85  => ERESTART       ,  // Interrupted system call should be restarted
        86  => ESTRPIPE       ,  // Streams pipe error
        87  => EUSERS         ,  // Too many users
        88  => ENOTSOCK       ,  // Socket operation on non-socket
        89  => EDESTADDRREQ   ,  // Destination address required
        90  => EMSGSIZE       ,  // Message too long
        91  => EPROTOTYPE     ,  // Protocol wrong type for socket
        92  => ENOPROTOOPT    ,  // Protocol not available
        93  => EPROTONOSUPPORT,  // Protocol not supported
        94  => ESOCKTNOSUPPORT,  // Socket type not supported
        95  => EOPNOTSUPP     ,  // Operation not supported on transport endpoint
        96  => EPFNOSUPPORT   ,  // Protocol family not supported
        97  => EAFNOSUPPORT   ,  // Address family not supported by protocol
        98  => EADDRINUSE     ,  // Address already in use
        99  => EADDRNOTAVAIL  ,  // Cannot assign requested address
        100 => ENETDOWN       ,  // Network is down
        101 => ENETUNREACH    ,  // Network is unreachable
        102 => ENETRESET      ,  // Network dropped connection because of reset
        103 => ECONNABORTED   ,  // Software caused connection abort
        104 => ECONNRESET     ,  // Connection reset by peer
        105 => ENOBUFS        ,  // No buffer space available
        106 => EISCONN        ,  // Transport endpoint is already connected
        107 => ENOTCONN       ,  // Transport endpoint is not connected
        108 => ESHUTDOWN      ,  // Cannot send after transport endpoint shutdown
        109 => ETOOMANYREFS   ,  // Too many references: cannot splice
        110 => ETIMEDOUT      ,  // Connection timed out
        111 => ECONNREFUSED   ,  // Connection refused
        112 => EHOSTDOWN      ,  // Host is down
        113 => EHOSTUNREACH   ,  // No route to host
        114 => EALREADY       ,  // Operation already in progress
        115 => EINPROGRESS    ,  // Operation now in progress
        116 => ESTALE         ,  // Stale NFS file handle
        117 => EUCLEAN        ,  // Structure needs cleaning
        118 => ENOTNAM        ,  // Not a XENIX named type file
        119 => ENAVAIL        ,  // No XENIX semaphores available
        120 => EISNAM         ,  // Is a named type file
        121 => EREMOTEIO      ,  // Remote I/O error
        122 => EDQUOT         ,  // Quota exceeded
        123 => ENOMEDIUM      ,  // No medium found
        124 => EMEDIUMTYPE    ,  // Wrong medium type
        125 => ECANCELED      ,  // Operation Canceled
        126 => ENOKEY         ,  // Required key not available
        127 => EKEYEXPIRED    ,  // Key has expired
        128 => EKEYREVOKED    ,  // Key has been revoked
        129 => EKEYREJECTED   ,  // Key was rejected by service
        130 => EOWNERDEAD     ,  // Owner died
        131 => ENOTRECOVERABLE,  // State not recoverable
        _   => UNKNOWN_ERRNO
    }
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
