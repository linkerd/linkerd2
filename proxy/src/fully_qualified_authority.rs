use bytes::BytesMut;

use std::net::IpAddr;
use std::str::FromStr;

use http::uri::Authority;

#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct FullyQualifiedAuthority(Authority);

impl FullyQualifiedAuthority {
    /// Normalizes the name according to Kubernetes service naming conventions.
    /// Case folding is not done; that is done internally inside `Authority`.
    ///
    /// This assumes the authority is syntactically valid.
    pub fn new(authority: &Authority, default_namespace: Option<&str>,
               default_zone: Option<&str>)
               -> FullyQualifiedAuthority {
        // Don't change IP-address-based authorities.
        if IpAddr::from_str(authority.host()).is_ok() {
            return FullyQualifiedAuthority(authority.clone())
        };

        // TODO: `Authority` doesn't have a way to get the serialized form of the
        // port, so do it ourselves.
        let (name, colon_port) = {
            let authority = authority.as_str();
            match authority.rfind(':') {
                Some(p) => authority.split_at(p),
                None => (authority, ""),
            }
        };

        // A fully qualified name ending with a dot is normalized by removing the
        // dot and doing nothing else.
        if name.ends_with('.') {
            let authority = authority.clone().into_bytes();
            let normalized = authority.slice(0, authority.len() - 1);
            return FullyQualifiedAuthority(Authority::from_shared(normalized).unwrap());
        }
        let mut parts = name.split('.');

        // `Authority` guarantees the name has at least one part.
        assert!(parts.next().is_some());

        // Rewrite "$name" -> "$name.$default_namespace".
        let has_explicit_namespace = parts.next().is_some();
        let namespace_to_append = if !has_explicit_namespace {
            default_namespace
        } else {
            None
        };

        // Rewrite "$name.$namespace" -> "$name.$namespace.svc".
        let (has_svc, append_svc) = if let Some(part) = parts.next() {
            (part.eq_ignore_ascii_case("svc"), false)
        } else {
            let has_namespace =
                has_explicit_namespace || namespace_to_append.is_some();
            (has_namespace, has_namespace)
        };

        // Rewrite "$name.$namespace.svc" -> "$name.$namespace.svc.$zone".
        let zone_to_append = if has_svc && parts.next().is_none() {
            default_zone
        } else {
            None
        };

        let mut additional_len = 0;
        if let Some(namespace) = namespace_to_append {
            additional_len += 1 + namespace.len(); // "." + namespace
        }
        if append_svc {
            additional_len += 4; // ".svc"
        }
        if let Some(zone) = zone_to_append {
            additional_len += 1 + zone.len(); // "." + zone
        }

        // If we're not going to change anything then don't allocate anything.
        if additional_len == 0 {
            return FullyQualifiedAuthority(authority.clone());
        }

        // `authority.as_str().len()` includes the length of `colon_port`.
        let mut normalized =
            BytesMut::with_capacity(authority.as_str().len() + additional_len);
        normalized.extend_from_slice(name.as_bytes());
        if let Some(namespace) = namespace_to_append {
            normalized.extend_from_slice(b".");
            normalized.extend_from_slice(namespace.as_bytes());
        }
        if append_svc {
            normalized.extend_from_slice(b".svc");
        }
        if let Some(zone) = zone_to_append {
            normalized.extend_from_slice(b".");
            normalized.extend_from_slice(zone.as_bytes());
        }
        normalized.extend_from_slice(colon_port.as_bytes());

        FullyQualifiedAuthority(Authority::from_shared(normalized.freeze())
            .expect("syntactically-valid authority"))
    }

    pub fn without_trailing_dot(&self) -> &str {
        self.0.as_str()
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn test_normalized_authority() {
        fn f(input: &str, default_namespace: Option<&str>,
             default_zone: Option<&str>)
             -> String {
            use bytes::Bytes;
            use http::uri::Authority;

            let input = Authority::from_shared(Bytes::from(input.as_bytes())).unwrap();
            let output = super::FullyQualifiedAuthority::new(
                &input, default_namespace, default_zone);
            output.without_trailing_dot().into()
        }

        assert_eq!("name",
                   f("name", None, None));
        assert_eq!("name.namespace.svc",
                   f("name.namespace", None, None));
        assert_eq!("name.namespace.svc",
                   f("name.namespace.svc", None, None));
        assert_eq!("name.namespace.svc.cluster",
                   f("name.namespace.svc.cluster", None, None));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc.cluster.local", None, None));

        assert_eq!("name.namespace.svc",
                   f("name", Some("namespace"), None));
        assert_eq!("name.namespace.svc",
                   f("name.namespace", Some("namespace"), None));
        assert_eq!("name.namespace.svc",
                   f("name.namespace.svc", Some("namespace"), None));
        assert_eq!("name.namespace.svc.cluster",
                   f("name.namespace.svc.cluster", Some("namespace"), None));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc.cluster.local", Some("namespace"), None));

        assert_eq!("name",
                   f("name", None, Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace", None, Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc", None, Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster",
                   f("name.namespace.svc.cluster", None, Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc.cluster.local", None, Some("cluster.local")));

        assert_eq!("name.namespace.svc.cluster.local",
                   f("name", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster",
                   f("name.namespace.svc.cluster", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc.cluster.local", Some("namespace"), Some("cluster.local")));

        // Fully-qualified names end with a dot and aren't modified except by removing the dot.
        assert_eq!("name",
                   f("name.", None, None));
        assert_eq!("name.namespace",
                   f("name.namespace.", None, None));
        assert_eq!("name.namespace.svc",
                   f("name.namespace.svc.", None, None));
        assert_eq!("name.namespace.svc.cluster",
                   f("name.namespace.svc.cluster.", None, None));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc.cluster.local.", None, None));
        assert_eq!("name",
                   f("name.", Some("namespace"), None));
        assert_eq!("name.namespace",
                   f("name.namespace.", Some("namespace"), None));
        assert_eq!("name.namespace.svc",
                   f("name.namespace.svc.", Some("namespace"), None));
        assert_eq!("name.namespace.svc.cluster",
                   f("name.namespace.svc.cluster.", Some("namespace"), None));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc.cluster.local.", Some("namespace"), None));
        assert_eq!("name",
                   f("name.", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace",
                   f("name.namespace.", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc",
                   f("name.namespace.svc.", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster",
                   f("name.namespace.svc.cluster.", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local",
                   f("name.namespace.svc.cluster.local.", Some("namespace"), Some("cluster.local")));

        // Ports are preserved.
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   f("name:1234", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   f("name.namespace:1234", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   f("name.namespace.svc:1234", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster:1234",
                   f("name.namespace.svc.cluster:1234", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   f("name.namespace.svc.cluster.local:1234", Some("namespace"), Some("cluster.local")));

        // "SVC" is recognized as being equivalent to "svc"
        assert_eq!("name.namespace.SVC.cluster.local",
                   f("name.namespace.SVC", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.SVC.cluster",
                   f("name.namespace.SVC.cluster", Some("namespace"), Some("cluster.local")));
        assert_eq!("name.namespace.SVC.cluster.local",
                   f("name.namespace.SVC.cluster.local", Some("namespace"), Some("cluster.local")));

        // IPv4 addresses are left unchanged.
        assert_eq!("1.2.3.4",
                   f("1.2.3.4", Some("namespace"), Some("cluster.local")));
        assert_eq!("1.2.3.4:1234",
                   f("1.2.3.4:1234", Some("namespace"), Some("cluster.local")));

        // IPv6 addresses are left unchanged.
        assert_eq!("[::1]",
                   f("[::1]", Some("namespace"), Some("cluster.local")));
        assert_eq!("[::1]:1234",
                   f("[::1]:1234", Some("namespace"), Some("cluster.local")));
    }
}
