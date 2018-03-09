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

        // parts should have a maximum 4 of pieces (name, namespace, svc, zone)
        let mut parts = name.splitn(4, '.');

        // `Authority` guarantees the name has at least one part.
        assert!(parts.next().is_some());

        // Rewrite "$name" -> "$name.$default_namespace".
        let has_explicit_namespace = match parts.next() {
            Some("") => {
                // "$name." is an external absolute name.
                return NamedAddress {
                    name: FullyQualifiedAuthority(authority.clone()),
                    use_destination_service: false,
                };
            },
            Some(_) => true,
            None => false,
        };
        let namespace_to_append = if !has_explicit_namespace {
            Some(default_namespace)
        } else {
            None
        };

        // Rewrite "$name.$namespace" -> "$name.$namespace.svc".
        let append_svc = if let Some(part) = parts.next() {
            if !part.eq_ignore_ascii_case("svc") {
                // If not "$name.$namespace.svc", treat as external.
                return NamedAddress {
                    name: FullyQualifiedAuthority(authority.clone()),
                    use_destination_service: false,
                };
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
        let (zone_to_append, strip_last) = if let Some(zone) = parts.next() {
            let (zone, strip_last) =
                if zone.ends_with('.') {
                    (&zone[..zone.len() - 1], true)
                } else {
                    (zone, false)
                };
            if !zone.eq_ignore_ascii_case(DEFAULT_ZONE) {
                // "a.b.svc." is an external absolute name.
                // "a.b.svc.foo" is external if the default zone is not
                // "foo".
                return NamedAddress {
                    name: FullyQualifiedAuthority(authority.clone()),
                    use_destination_service: false,
                }
            }
            (None, strip_last)
        } else {
            (Some(DEFAULT_ZONE), false)
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
        if additional_len == 0 && !strip_last {
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

        if strip_last {
            let new_len = normalized.len() - 1;
            normalized.truncate(new_len);
        }

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
            assert_eq!(output.use_destination_service, true, "input: {}", input);
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
        external("name.", "namespace");
        external("name.namespace.", "namespace");
        external("name.namespace.svc.", "namespace");
        external("name.namespace.svc.cluster.", "namespace");
        external("name.namespace.svc.acluster.local.", "namespace");
        assert_eq!("name.namespace.svc.cluster.local",
                   local("name.namespace.svc.cluster.local.", "namespace"));

        // Irrespective of how other absolute names are resolved, "localhost."
        // absolute names aren't ever resolved through the destination service,
        // as prescribed by https://tools.ietf.org/html/rfc6761#section-6.3:
        //
        //     The domain "localhost." and any names falling within ".localhost."
        //     are special in the following ways: [...]
        //
        //     Name resolution APIs and libraries SHOULD recognize localhost
        //     names as special and SHOULD always return the IP loopback address
        //     for address queries [...] Name resolution APIs SHOULD NOT send
        //     queries for localhost names to their configured caching DNS server(s).
        external("localhost.", "namespace");
        external("name.localhost.", "namespace");
        external("name.namespace.svc.localhost.", "namespace");

        // Although it probably isn't the desired behavior in almost any circumstance, match
        // standard behavior for non-absolute "localhost" and names that end with
        // ".localhost" at least until we're comfortable implementing
        // https://wiki.tools.ietf.org/html/draft-ietf-dnsop-let-localhost-be-localhost.
        assert_eq!("localhost.namespace.svc.cluster.local",
                   local("localhost", "namespace"));
        assert_eq!("name.localhost.svc.cluster.local",
                   local("name.localhost", "namespace"));

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
