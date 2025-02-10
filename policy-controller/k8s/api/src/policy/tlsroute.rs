use gateway_api::apis::experimental::tlsroutes::{TLSRouteParentRefs, TLSRouteRulesBackendRefs};

pub fn parent_ref_targets_kind<T>(parent_ref: &TLSRouteParentRefs) -> bool
where
    T: kube::Resource,
    T::DynamicType: Default,
{
    let kind = match parent_ref.kind {
        Some(ref kind) => kind,
        None => return false,
    };

    super::targets_kind::<T>(parent_ref.group.as_deref(), kind)
}

pub fn backend_ref_targets_kind<T>(backend_ref: &TLSRouteRulesBackendRefs) -> bool
where
    T: kube::Resource,
    T::DynamicType: Default,
{
    // Default kind is assumed to be service for backend ref objects
    super::targets_kind::<T>(
        backend_ref.group.as_deref(),
        backend_ref.kind.as_deref().unwrap_or("Service"),
    )
}
