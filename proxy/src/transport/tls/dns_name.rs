use super::{untrusted, webpki};
use std::fmt;
use convert::TryFrom;

/// A `DnsName` is guaranteed to be syntactically valid. The validity rules
/// are specified in [RFC 5280 Section 7.2], except that underscores are also
/// allowed.
#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub struct DnsName(pub(super) webpki::DNSName);

impl fmt::Display for DnsName {
    fn fmt(&self, f: &mut fmt::Formatter) -> Result<(), fmt::Error> {
        self.as_ref().fmt(f)
    }
}

#[derive(Debug, Eq, PartialEq)]
pub struct InvalidDnsName;

impl<'a> TryFrom<&'a [u8]> for DnsName {
    type Err = InvalidDnsName;
    fn try_from(s: &[u8]) -> Result<Self, Self::Err> {
        webpki::DNSNameRef::try_from_ascii(untrusted::Input::from(s))
            .map(|r| DnsName(r.to_owned()))
            .map_err(|()| InvalidDnsName)
    }
}

impl AsRef<str> for DnsName {
    fn as_ref(&self) -> &str {
        <webpki::DNSName as AsRef<str>>::as_ref(&self.0)
    }
}
