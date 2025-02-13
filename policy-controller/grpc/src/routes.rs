use linkerd2_proxy_api::{http_route as proto, http_types, tls_route as tls_proto};
use linkerd_policy_controller_core::routes::{
    HeaderModifierFilter, HostMatch, PathModifier, RequestRedirectFilter,
};

pub(crate) mod grpc;
pub(crate) mod http;

pub(crate) fn convert_host_match(h: HostMatch) -> proto::HostMatch {
    proto::HostMatch {
        r#match: Some(match h {
            HostMatch::Exact(host) => proto::host_match::Match::Exact(host),
            HostMatch::Suffix { reverse_labels } => {
                proto::host_match::Match::Suffix(proto::host_match::Suffix {
                    reverse_labels: reverse_labels.to_vec(),
                })
            }
        }),
    }
}

pub(crate) fn convert_sni_match(h: HostMatch) -> tls_proto::SniMatch {
    tls_proto::SniMatch {
        r#match: Some(match h {
            HostMatch::Exact(host) => tls_proto::sni_match::Match::Exact(host),
            HostMatch::Suffix { reverse_labels } => {
                tls_proto::sni_match::Match::Suffix(tls_proto::sni_match::Suffix {
                    reverse_labels: reverse_labels.to_vec(),
                })
            }
        }),
    }
}

pub(crate) fn convert_request_header_modifier_filter(
    HeaderModifierFilter { add, set, remove }: HeaderModifierFilter,
) -> proto::RequestHeaderModifier {
    proto::RequestHeaderModifier {
        add: Some(http_types::Headers {
            headers: add
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        set: Some(http_types::Headers {
            headers: set
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        remove: remove.into_iter().map(|n| n.to_string()).collect(),
    }
}

pub(crate) fn convert_response_header_modifier_filter(
    HeaderModifierFilter { add, set, remove }: HeaderModifierFilter,
) -> proto::ResponseHeaderModifier {
    proto::ResponseHeaderModifier {
        add: Some(http_types::Headers {
            headers: add
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        set: Some(http_types::Headers {
            headers: set
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        remove: remove.into_iter().map(|n| n.to_string()).collect(),
    }
}

pub(crate) fn convert_redirect_filter(
    RequestRedirectFilter {
        scheme,
        host,
        path,
        port,
        status,
    }: RequestRedirectFilter,
) -> proto::RequestRedirect {
    proto::RequestRedirect {
        scheme: scheme.map(|ref s| {
            if let Some(s) = http_types::scheme::Registered::from_str_name(s.as_str()) {
                http_types::Scheme {
                    r#type: Some(http_types::scheme::Type::Registered(s.into())),
                }
            } else {
                http_types::Scheme {
                    r#type: Some(http_types::scheme::Type::Unregistered(s.to_string())),
                }
            }
        }),
        host: host.unwrap_or_default(),
        path: path.map(|pm| proto::PathModifier {
            replace: Some(match pm {
                PathModifier::Full(p) => proto::path_modifier::Replace::Full(p),
                PathModifier::Prefix(p) => proto::path_modifier::Replace::Prefix(p),
            }),
        }),
        port: port.map(u16::from).map(u32::from).unwrap_or_default(),
        status: u32::from(status.unwrap_or_default().as_u16()),
    }
}
