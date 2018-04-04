use bytes::{BytesMut};

use transport::DnsNameAndPort;

/// A normalized `Authority`.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct FullyQualifiedAuthority(String);

impl FullyQualifiedAuthority {
    /// Normalizes the name according to Kubernetes service naming conventions.
    /// Case folding is not done; that is done internally inside `Authority`.
    pub fn normalize(authority: &DnsNameAndPort, default_namespace: &str) -> Option<Self> {
        let name: &str = authority.host.as_ref();

        // parts should have a maximum 4 of pieces (name, namespace, svc, zone)
        let mut parts = name.splitn(4, '.');

        // `dns::Name` guarantees the name has at least one part.
        assert!(parts.next().is_some());

        // Rewrite "$name" -> "$name.$default_namespace".
        let has_explicit_namespace = match parts.next() {
            Some("") => {
                // "$name." is an external absolute name.
                return None;
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
                return None;
            }
            false
        } else if has_explicit_namespace {
            true
        } else if namespace_to_append.is_none() {
            // We can't append ".svc" without a namespace, so treat as external.
            return None;
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
                return None;
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

        let port_str_len = match authority.port {
            80 => 0, // XXX: Assumes http://, which is all we support right now.
            p if p >= 10000 => 1 + 5,
            p if p >= 1000 => 1 + 4,
            p if p >= 100 => 1 + 3,
            p if p >= 10 => 1 + 2,
            _ => 1,
        };

        let mut normalized = BytesMut::with_capacity(name.len() + additional_len + port_str_len);
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

        if strip_last {
            let new_len = normalized.len() - 1;
            normalized.truncate(new_len);
        }

        // Append the port
        if port_str_len > 0 {
            normalized.extend_from_slice(b":");
            let port = authority.port.to_string();
            normalized.extend_from_slice(port.as_ref());
        }

        Some(FullyQualifiedAuthority(String::from_utf8(normalized.freeze().to_vec()).unwrap()))
    }

    pub fn without_trailing_dot(&self) -> &str {
        &self.0
    }
}

#[cfg(test)]
mod tests {
    use transport::{DnsNameAndPort, Host, HostAndPort};
    use http::uri::Authority;
    use std::str::FromStr;

    #[test]
    fn test_normalized_authority() {
        fn dns_name_and_port_from_str(input: &str) -> DnsNameAndPort {
            let authority = Authority::from_str(input).unwrap();
            match HostAndPort::normalize(&authority, Some(80)) {
                Ok(HostAndPort { host: Host::DnsName(host), port }) =>
                    DnsNameAndPort { host, port },
                Err(e) => {
                    unreachable!("{:?} when parsing {:?}", e, input)
                },
                _ => unreachable!("Not a DNS name: {:?}", input),
            }
        }

        fn local(input: &str, default_namespace: &str) -> String {
            let name = dns_name_and_port_from_str(input);
            let output = super::FullyQualifiedAuthority::normalize(&name, default_namespace);
            assert!(output.is_some(), "input: {}", input);
            output.unwrap().without_trailing_dot().into()
        }

        fn external(input: &str, default_namespace: &str) {
            let name = dns_name_and_port_from_str(input);
            let output = super::FullyQualifiedAuthority::normalize(&name, default_namespace);
            assert!(output.is_none(), "input: {}", input);
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
        assert_eq!("name.namespace.svc.cluster.local",
                   local("name.namespace.SVC", "namespace"));
        external("name.namespace.SVC.cluster", "namespace");
        assert_eq!("name.namespace.svc.cluster.local",
                   local("name.namespace.SVC.cluster.local", "namespace"));
    }
}
