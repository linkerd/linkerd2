pub mod index;

pub use index::{Index, ServiceRef, SharedIndex};
use linkerd_policy_controller_core::outbound::{RetryPolicy, StatusRange};
use linkerd_policy_controller_k8s_api::policy::HttpRetryFilter;

pub fn retry_filter(filter: HttpRetryFilter) -> Result<RetryPolicy, anyhow::Error> {
    use std::num::NonZeroU32;
    let statuses = filter
        .spec
        .retry_statuses
        .map(|statuses| {
            statuses
                .iter()
                .map(|s| s.parse::<StatusRange>())
                .collect::<Result<_, _>>()
        })
        .transpose()?
        .unwrap_or_else(|| {
            vec![StatusRange {
                min: http::StatusCode::INTERNAL_SERVER_ERROR,
                max: http::StatusCode::from_u16(599).unwrap(),
            }]
        });
    let max_per_request = filter
        .spec
        .max_retries_per_request
        .and_then(NonZeroU32::new);

    Ok(RetryPolicy {
        max_per_request,
        statuses,
    })
}
