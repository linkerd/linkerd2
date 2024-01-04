use anyhow::Result;
use thiserror::Error;

use regex::Regex;

const SCHEME_PREFIX: &str = "spiffe://";
const VALID_TRUST_DOMAIN_CHARS: &str = "abcdefghijklmnopqrstuvwxyz0123456789-._";
const VALID_PATH_SEGMENT_CHARS: &str =
    "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._";

const DNS_LIKE_IDENTITY_REGEX: &str =
    r"^(\*|[a-z0-9]([-a-z0-9]*[a-z0-9])?)(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$";

#[derive(Debug, Error, PartialEq, Clone)]
pub enum IdError {
    /// The trust domain name of SPIFFE ID cannot be empty.
    #[error("SPIFFE trust domain is missing")]
    MissingTrustDomain,

    /// A trust domain name can only contain chars in a limited char set.
    #[error(
        "SPIFFE trust domain characters are limited to lowercase letters, numbers, dots, dashes, and \
         underscores"
    )]
    BadTrustDomainChar,

    /// A path segment can only contain chars in a limited char set.
    #[error(
        "SPIFFE path segment characters are limited to letters, numbers, dots, dashes, and underscores"
    )]
    BadPathSegmentChar,

    /// Path cannot contain empty segments, e.g '//'
    #[error("SPIFFE path cannot contain empty segments")]
    EmptySegment,

    /// Path cannot contain dot segments, e.g '/.', '/..'
    #[error("SPIFFE path cannot contain dot segments")]
    DotSegment,

    /// Path cannot have a trailing slash.
    #[error("SPIFFE path cannot have a trailing slash")]
    TrailingSlash,

    #[error(
        "identity must be a valid SPIFFE id or a DNS SAN, matching the regex: {}",
        DNS_LIKE_IDENTITY_REGEX
    )]
    Invalid,
}

// Validates that an ID is either in DNS or SPIFFE form. SPIFFE
// validation is based on https://github.com/spiffe/spiffe/blob/27b59b81ba8c56885ac5d4be73b35b9b3305fd7a/standards/SPIFFE-ID.md.
// Implementation is based on: https://github.com/maxlambrecht/rust-spiffe/blob/3d3614f70d0d7a4b9190ab9650e224f2ac362368/spiffe/src/spiffe_id/mod.rs
pub(crate) fn validate_identity(id: &str) -> Result<(), IdError> {
    if let Some(rest) = id.strip_prefix(SCHEME_PREFIX) {
        let i = rest.find('/').unwrap_or(rest.len());

        if i == 0 {
            return Err(IdError::MissingTrustDomain);
        }

        let td = &rest[..i];
        if td.chars().any(|c| !VALID_TRUST_DOMAIN_CHARS.contains(c)) {
            return Err(IdError::BadTrustDomainChar);
        }

        let path = &rest[i..];

        if !path.is_empty() {
            validate_path(path)?;
        }

        Ok(())
    } else {
        let regex = Regex::new(DNS_LIKE_IDENTITY_REGEX).expect("should_compile");
        if !regex.is_match(id) {
            return Err(IdError::Invalid);
        }
        Ok(())
    }
}

/// Validates that a path string is a conformant path for a SPIFFE ID.
/// See https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE-ID.md#22-path
fn validate_path(path: &str) -> Result<(), IdError> {
    let chars = path.char_indices().peekable();
    let mut segment_start = 0;

    for (idx, c) in chars {
        if c == '/' {
            match &path[segment_start..idx] {
                "/" => return Err(IdError::EmptySegment),
                "/." | "/.." => return Err(IdError::DotSegment),
                _ => {}
            }
            segment_start = idx;
            continue;
        }

        if !VALID_PATH_SEGMENT_CHARS.contains(c) {
            return Err(IdError::BadPathSegmentChar);
        }
    }

    match &path[segment_start..] {
        "/" => return Err(IdError::TrailingSlash),
        "/." | "/.." => return Err(IdError::DotSegment),
        _ => {}
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn valid_dns() {
        assert!(validate_identity("system.local").is_ok())
    }

    #[test]
    fn valid_dns_all() {
        assert!(validate_identity("*").is_ok())
    }

    #[test]
    fn valid_dns_prefix() {
        assert!(validate_identity("*.system.local").is_ok())
    }

    #[test]
    fn invalid_dns_suffix() {
        let err = validate_identity("system.local.*").unwrap_err();
        assert_eq!(err, IdError::Invalid);
    }

    #[test]
    fn invalid_dns_trailing_dot() {
        let err = validate_identity("system.local.").unwrap_err();
        assert_eq!(err, IdError::Invalid);
    }

    #[test]
    fn invalid_dns_leading_dot() {
        let err = validate_identity(".system.local").unwrap_err();
        assert_eq!(err, IdError::Invalid);
    }

    #[test]
    fn invalid_dns_double_dots() {
        let err = validate_identity("system..local").unwrap_err();
        assert_eq!(err, IdError::Invalid);
    }

    #[test]
    fn valid_spiffe_no_path() {
        assert!(validate_identity("spiffe://trustdomain").is_ok())
    }

    #[test]
    fn valid_spiffe_with_path() {
        assert!(validate_identity("spiffe://trustdomain/path/element").is_ok())
    }

    #[test]
    fn invalid_spiffe_scheme() {
        let err = validate_identity("http://domain.test/path/element").unwrap_err();
        assert_eq!(err, IdError::Invalid);
    }

    #[test]
    fn invalid_spiffe_wrong_scheme() {
        let err = validate_identity("spiffe:/path/element").unwrap_err();
        assert_eq!(err, IdError::Invalid);
    }

    #[test]
    fn invalid_spiffe_empty_trust_domain() {
        let err = validate_identity("spiffe:///path/element").unwrap_err();
        assert_eq!(err, IdError::MissingTrustDomain);
    }

    #[test]
    fn invalid_spiffe_no_slashes_in_scheme() {
        let err = validate_identity("spiffe:path/element").unwrap_err();
        assert_eq!(err, IdError::Invalid);
    }

    #[test]
    fn invalid_spiffe_uri_with_query() {
        let err = validate_identity("spiffe://domain.test/path/element?query=").unwrap_err();
        assert_eq!(err, IdError::BadPathSegmentChar);
    }

    #[test]
    fn invalid_spiffe_uri_with_fragment() {
        let err = validate_identity("spiffe://domain.test/path/element#fragment-1").unwrap_err();
        assert_eq!(err, IdError::BadPathSegmentChar);
    }

    #[test]
    fn invalid_spiffe_uri_with_str_port() {
        let err = validate_identity("spiffe://domain.test:8080/path/element").unwrap_err();
        assert_eq!(err, IdError::BadTrustDomainChar);
    }

    #[test]
    fn invalid_spiffe_uri_with_user_info() {
        let err = validate_identity("spiffe://user:password@test.org/path/element").unwrap_err();
        assert_eq!(err, IdError::BadTrustDomainChar);
    }

    #[test]
    fn invalid_spiffe_uri_with_trailing_slash() {
        let err = validate_identity("spiffe://test.org/").unwrap_err();
        assert_eq!(err, IdError::TrailingSlash);
    }

    #[test]
    fn invalid_spiffe_uri_with_empty_segment() {
        let err = validate_identity("spiffe://test.org//").unwrap_err();
        assert_eq!(err, IdError::EmptySegment);
    }

    #[test]
    fn invalid_spiffe_uri_str_with_path_with_trailing_slash() {
        let err = validate_identity("spiffe://test.org/path/other/").unwrap_err();
        assert_eq!(err, IdError::TrailingSlash);
    }

    #[test]
    fn invalid_spiffe_uri_str_with_dot_segment() {
        let err = validate_identity("spiffe://test.org/./other").unwrap_err();
        assert_eq!(err, IdError::DotSegment);
    }

    #[test]
    fn invalid_spiffe_uri_str_with_double_dot_segment() {
        let err = validate_identity("spiffe://test.org/../other").unwrap_err();
        assert_eq!(err, IdError::DotSegment);
    }
}
