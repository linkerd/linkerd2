use anyhow::{bail, Context, Result};
use std::num::NonZeroU16;

/// A `HashSet` specialized for ports.
///
/// Because ports are `u16` values, this type avoids the overhead of actually
/// hashing ports.
pub type PortSet = std::collections::HashSet<NonZeroU16, std::hash::BuildHasherDefault<PortHasher>>;

/// A `HashMap` specialized for ports.
///
/// Because ports are `NonZeroU16` values, this type avoids the overhead of
/// actually hashing ports.
pub(crate) type PortMap<V> =
    std::collections::HashMap<NonZeroU16, V, std::hash::BuildHasherDefault<PortHasher>>;

/// A hasher for ports.
///
/// Because ports are single `NonZeroU16` values, we don't have to hash them; we can just use
/// the integer values as hashes directly.
///
/// Borrowed from the proxy.
#[derive(Debug, Default)]
pub struct PortHasher(u16);

// === impl PortHasher ===

impl std::hash::Hasher for PortHasher {
    fn write(&mut self, _: &[u8]) {
        unreachable!("hashing a `u16` calls `write_u16`");
    }

    #[inline]
    fn write_u16(&mut self, port: u16) {
        self.0 = port;
    }

    #[inline]
    fn finish(&self) -> u64 {
        self.0 as u64
    }
}

/// Reads `annotation` from the provided set of annotations, parsing it as a port set.  If the
/// annotation is not set or is invalid, the empty set is returned.
pub(crate) fn ports_annotation(
    annotations: &std::collections::BTreeMap<String, String>,
    annotation: &str,
) -> Option<PortSet> {
    annotations.get(annotation).map(|spec| {
        parse_portset(spec).unwrap_or_else(|error| {
            tracing::info!(%spec, %error, %annotation, "Invalid ports list");
            Default::default()
        })
    })
}

/// Read a comma-separated of ports or port ranges from the given string.
pub fn parse_portset(s: &str) -> Result<PortSet> {
    let mut ports = PortSet::default();

    for spec in s.split(',') {
        match spec.split_once('-') {
            None => {
                if !spec.trim().is_empty() {
                    let port = spec.trim().parse().context("parsing port")?;
                    ports.insert(port);
                }
            }
            Some((floor, ceil)) => {
                let floor = floor.trim().parse::<NonZeroU16>().context("parsing port")?;
                let ceil = ceil.trim().parse::<NonZeroU16>().context("parsing port")?;
                if floor > ceil {
                    bail!("Port range must be increasing");
                }
                ports.extend(
                    (u16::from(floor)..=u16::from(ceil)).map(|p| NonZeroU16::try_from(p).unwrap()),
                );
            }
        }
    }

    Ok(ports)
}

#[cfg(test)]
mod tests {
    use super::*;

    macro_rules! ports {
        ($($x:expr),+ $(,)?) => (
            vec![$($x),+]
                .into_iter()
                .map(NonZeroU16::try_from)
                .collect::<Result<PortSet, _>>()
                .unwrap()
        );
    }

    #[test]
    fn parse_portset() {
        use super::parse_portset;

        assert!(parse_portset("").unwrap().is_empty(), "empty");
        assert!(parse_portset("0").is_err(), "0");
        assert_eq!(parse_portset("1").unwrap(), ports![1], "1");
        assert_eq!(parse_portset("1-3").unwrap(), ports![1, 2, 3], "1-2");
        assert_eq!(parse_portset("4,1-2").unwrap(), ports![1, 2, 4], "4,1-2");
        assert!(parse_portset("2-1").is_err(), "2-1");
        assert!(parse_portset("2-").is_err(), "2-");
        assert!(parse_portset("65537").is_err(), "65537");
    }
}
