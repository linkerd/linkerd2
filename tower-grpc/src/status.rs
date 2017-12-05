use std::fmt;

use h2;
use http::header::HeaderValue;

#[derive(Debug, Clone)]
pub struct Status {
    code: Code,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub struct Code(Code_);

impl Status {
    #[inline]
    pub fn code(&self) -> Code {
        self.code
    }

    pub const OK: Status = Status {
        code: Code(Code_::Ok),
    };

    pub const CANCELED: Status = Status {
        code: Code(Code_::Canceled),
    };

    pub const UNKNOWN: Status = Status {
        code: Code(Code_::Unknown),
    };

    pub const INVALID_ARGUMENT: Status = Status {
        code: Code(Code_::InvalidArgument),
    };

    pub const DEADLINE_EXCEEDED: Status = Status {
        code: Code(Code_::DeadlineExceeded),
    };

    pub const NOT_FOUND: Status = Status {
        code: Code(Code_::NotFound),
    };

    pub const ALREADY_EXISTS: Status = Status {
        code: Code(Code_::AlreadyExists),
    };

    pub const PERMISSION_DENIED: Status = Status {
        code: Code(Code_::PermissionDenied),
    };

    pub const RESOURCE_EXHAUSTED: Status = Status {
        code: Code(Code_::ResourceExhausted),
    };

    pub const FAILED_PRECONDITION: Status = Status {
        code: Code(Code_::FailedPrecondition),
    };

    pub const ABORTED: Status = Status {
        code: Code(Code_::Aborted),
    };

    pub const OUT_OF_RANGE: Status = Status {
        code: Code(Code_::OutOfRange),
    };

    pub const UNIMPLEMENTED: Status = Status {
        code: Code(Code_::Unimplemented),
    };

    pub const INTERNAL: Status = Status {
        code: Code(Code_::Internal),
    };

    pub const UNAVAILABLE: Status = Status {
        code: Code(Code_::Unavailable),
    };

    pub const DATA_LOSS: Status = Status {
        code: Code(Code_::DataLoss),
    };

    pub const UNAUTHENTICATED: Status = Status {
        code: Code(Code_::Unauthenticated),
    };

    pub(crate) fn from_bytes(bytes: &[u8]) -> Status {
        let code = match bytes.len() {
            1 => {
                match bytes[0] {
                    b'0' => Code_::Ok,
                    b'1' => Code_::Canceled,
                    b'2' => Code_::Unknown,
                    b'3' => Code_::InvalidArgument,
                    b'4' => Code_::DeadlineExceeded,
                    b'5' => Code_::NotFound,
                    b'6' => Code_::AlreadyExists,
                    b'7' => Code_::PermissionDenied,
                    b'8' => Code_::ResourceExhausted,
                    b'9' => Code_::FailedPrecondition,
                    _ => return Status::parse_err(),
                }
            },
            2 => {
                match (bytes[0], bytes[1]) {
                    (b'1', b'0') => Code_::Aborted,
                    (b'1', b'1') => Code_::OutOfRange,
                    (b'1', b'2') => Code_::Unimplemented,
                    (b'1', b'3') => Code_::Internal,
                    (b'1', b'4') => Code_::Unavailable,
                    (b'1', b'5') => Code_::DataLoss,
                    (b'1', b'6') => Code_::Unauthenticated,
                    _ => return Status::parse_err(),
                }
            },
            _ => return Status::parse_err(),
        };

        Status::new(Code(code))
    }

    // TODO: It would be nice for this not to be public
    pub fn to_header_value(&self) -> HeaderValue {
        use self::Code_::*;

        match self.code.0 {
            Ok => HeaderValue::from_static("0"),
            Canceled => HeaderValue::from_static("1"),
            Unknown => HeaderValue::from_static("2"),
            InvalidArgument => HeaderValue::from_static("3"),
            DeadlineExceeded => HeaderValue::from_static("4"),
            NotFound => HeaderValue::from_static("5"),
            AlreadyExists => HeaderValue::from_static("6"),
            PermissionDenied => HeaderValue::from_static("7"),
            ResourceExhausted => HeaderValue::from_static("8"),
            FailedPrecondition => HeaderValue::from_static("9"),
            Aborted => HeaderValue::from_static("10"),
            OutOfRange => HeaderValue::from_static("11"),
            Unimplemented => HeaderValue::from_static("12"),
            Internal => HeaderValue::from_static("13"),
            Unavailable => HeaderValue::from_static("14"),
            DataLoss => HeaderValue::from_static("15"),
            Unauthenticated => HeaderValue::from_static("16"),
        }
    }

    fn new(code: Code) -> Status {
        Status {
            code,
        }
    }

    fn parse_err() -> Status {
        trace!("error parsing grpc-status");
        Status::UNKNOWN
    }
}

impl From<h2::Error> for Status {
    fn from(_err: h2::Error) -> Self {
        //TODO: https://grpc.io/docs/guides/wire.html#errors
        Status::new(Code(Code_::Internal))
    }
}

impl From<Status> for h2::Error {
    fn from(_status: Status) -> Self {
        // TODO: implement
        h2::Reason::INTERNAL_ERROR.into()
    }
}

impl Code {
    pub const OK: Code = Code(Code_::Ok);
    //TODO: the rest...
}

impl fmt::Debug for Code {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        fmt::Debug::fmt(&self.0, f)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Code_ {
    Ok = 0,
    Canceled = 1,
    Unknown = 2,
    InvalidArgument = 3,
    DeadlineExceeded = 4,
    NotFound = 5,
    AlreadyExists = 6,
    PermissionDenied = 7,
    ResourceExhausted = 8,
    FailedPrecondition = 9,
    Aborted = 10,
    OutOfRange = 11,
    Unimplemented = 12,
    Internal = 13,
    Unavailable = 14,
    DataLoss = 15,
    Unauthenticated = 16,
}
