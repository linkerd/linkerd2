use std::fmt;
use super::{
    Counter,
    HandshakeFailLabels,
    Metric,
    Scopes,
};

pub(super) type HandshakeFailScopes = Scopes<HandshakeFailLabels, Counter>;

// ===== impl HandshakeFailScopes =====

impl HandshakeFailScopes {
    metrics! {
        tls_handshake_failure_total: Counter {
            "Total count of TLS handshake failures."
        }
    }
}

impl fmt::Display for HandshakeFailScopes {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        if self.scopes.is_empty() {
            return Ok(());
        }

        Self::tls_handshake_failure_total.fmt_help(f)?;
        Self::tls_handshake_failure_total.fmt_scopes(f, &self, |s| &s)?;

        Ok(())
    }
}
