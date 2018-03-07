use bytes::BytesMut;

use std::net::IpAddr;
use std::str::FromStr;

use http::uri::Authority;

/// A normalized `Authority`.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct FullyQualifiedAuthority(Authority);

#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct NamedAddress {
    pub name: FullyQualifiedAuthority,
    pub use_destination_service: bool
}

impl FullyQualifiedAuthority {
    /// Normalizes the name according to Kubernetes service naming conventions.
    /// Case folding is not done; that is done internally inside `Authority`.
    ///
    /// This assumes the authority is syntactically valid.
    pub fn normalize(authority: &Authority, default_namespace: &str)
                     -> NamedAddress
    {
        // Don't change IP-address-based authorities.
        if IpAddr::from_str(authority.host()).is_ok() {
            return NamedAddress {
                name: FullyQualifiedAuthority(authority.clone()),
                use_destination_service: false,
            }
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
            let authority = Authority::from_shared(normalized).unwrap();
            let name = FullyQualifiedAuthority(authority);
            return NamedAddress {
                name,
                use_destination_service: true,
            }
        }

        // parts should have a maximum 4 of pieces (name, namespace, svc, zone)
        let mut parts = name.splitn(4, '.');

        // `Authority` guarantees the name has at least one part.
        assert!(parts.next().is_some());

        // Rewrite "$name" -> "$name.$default_namespace".
        let has_explicit_namespace = parts.next().is_some();
        let namespace_to_append = if !has_explicit_namespace {
            Some(default_namespace)
        } else {
            None
        };

        // Rewrite "$name.$namespace" -> "$name.$namespace.svc".
        let append_svc = if let Some(part) = parts.next() {
            if !part.eq_ignore_ascii_case("svc") {
                // if not "$name.$namespace.svc", treat as external
                return NamedAddress {
                    name: FullyQualifiedAuthority(authority.clone()),
                    use_destination_service: false,
                }
            }

            false
        } else if has_explicit_namespace {
            true
        } else if namespace_to_append.is_none() {
            // We can't append ".svc" without a namespace, so treat as external.
            return NamedAddress {
                name: FullyQualifiedAuthority(authority.clone()),
                use_destination_service: false,
            }
        } else {
            true
        };

        // Rewrite "$name.$namespace.svc" -> "$name.$namespace.svc.$zone".
        static DEFAULT_ZONE: &str = "cluster.local"; // TODO: make configurable.
        let zone_to_append = if let Some(zone) = parts.next() {
            if !zone.eq_ignore_ascii_case(DEFAULT_ZONE) {
                // if "a.b.svc.foo" and zone is not "foo",
                // treat as external
                return NamedAddress {
                    name: FullyQualifiedAuthority(authority.clone()),
                    use_destination_service: false,
                }
            }
            None
        } else {
            Some(DEFAULT_ZONE)
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
            return NamedAddress {
                name: FullyQualifiedAuthority(authority.clone()),
                use_destination_service: true,
            }
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

        let name = Authority::from_shared(normalized.freeze())
            .expect("syntactically-valid authority");
        let name = FullyQualifiedAuthority(name);
        NamedAddress {
            name,
            use_destination_service: true,
        }
    }

    pub fn without_trailing_dot(&self) -> &Authority {
        &self.0
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn test_normalized_authority() {
        fn local(input: &str, default_namespace: &str) -> String {
            use bytes::Bytes;
            use http::uri::Authority;

            let input = Authority::from_shared(Bytes::from(input.as_bytes()))
                .unwrap();
            let output = super::FullyQualifiedAuthority::normalize(
                &input, default_namespace);
            assert_eq!(output.use_destination_service, true);
            output.name.without_trailing_dot().as_str().into()
        }

        fn external(input: &str, default_namespace: &str) {
            use bytes::Bytes;
            use http::uri::Authority;

            let input = Authority::from_shared(Bytes::from(input.as_bytes())).unwrap();
            let output = super::FullyQualifiedAuthority::normalize(
                &input, default_namespace);
            assert_eq!(output.use_destination_service, false);
            assert_eq!(output.name.without_trailing_dot().as_str(), input);
        }

        assert_eq!("name.namespace.svc.cluster.local", local("name", "namespace"));
        assert_eq!("name.namespace.svc.cluster.local", local("name.namespace", "namespace"));
        assert_eq!("name.namespace.svc.cluster.local",
                   local("name.namespace.svc", "namespace"));
        external("name.namespace.svc.cluster", "namespace");
        assert_eq!("name.namespace.svc.cluster.local",
                   local("name.namespace.svc.cluster.local", "namespace"));

        // Fully-qualified names end with a dot and aren't modified except by removing the dot.
        assert_eq!("name",
                   local("name.", "namespace"));
        assert_eq!("name.namespace",
                   local("name.namespace.", "namespace"));
        assert_eq!("name.namespace.svc",
                   local("name.namespace.svc.", "namespace"));
        assert_eq!("name.namespace.svc.cluster",
                   local("name.namespace.svc.cluster.", "namespace"));
        assert_eq!("name.namespace.svc.cluster.local",
                   local("name.namespace.svc.cluster.local.", "namespace"));

        // Ports are preserved.
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   local("name:1234", "namespace"));
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   local("name.namespace:1234", "namespace"));
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   local("name.namespace.svc:1234", "namespace"));
        external("name.namespace.svc.cluster:1234", "namespace");
        assert_eq!("name.namespace.svc.cluster.local:1234",
                   local("name.namespace.svc.cluster.local:1234", "namespace"));

        // "SVC" is recognized as being equivalent to "svc"
        assert_eq!("name.namespace.SVC.cluster.local",
                   local("name.namespace.SVC", "namespace"));
        external("name.namespace.SVC.cluster", "namespace");
        assert_eq!("name.namespace.SVC.cluster.local",
                   local("name.namespace.SVC.cluster.local", "namespace"));

        // IPv4 addresses are left unchanged.
        external("1.2.3.4", "namespace");
        external("1.2.3.4:1234", "namespace");
        external("127.0.0.1", "namespace");
        external("127.0.0.1:8080", "namespace");

        // IPv6 addresses are left unchanged.
        external("[::1]", "namespace");
        external("[::1]:1234", "namespace");
    }
}
