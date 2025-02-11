use crate::resource_id::ResourceId;

pub(crate) mod grpc;
pub(crate) mod http;
pub(crate) mod tcp;
pub(crate) mod tls;

/// Represents an xRoute's parent reference from its spec.
///
/// This is separate from the policy controller index's `InboundParentRef`
/// because it does not validate that the parent reference is not in another
/// namespace. This is something that should be relaxed in the future in the
/// policy controller's index, and we could then consider consolidating these
/// types into a single shared lib.
#[derive(Clone, Eq, PartialEq, Debug)]
pub enum ParentReference {
    Server(ResourceId),
    Service(ResourceId, Option<u16>),
    EgressNetwork(ResourceId, Option<u16>),
    UnknownKind,
}

#[derive(Clone, Eq, PartialEq, Debug)]
pub enum BackendReference {
    Service(ResourceId),
    EgressNetwork(ResourceId),
    Unknown,
}
