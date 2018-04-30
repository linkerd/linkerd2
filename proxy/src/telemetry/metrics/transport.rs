use std::fmt;
use std::time::Duration;

use super::{
    latency,
    Counter,
    Gauge,
    Histogram,
    Metric,
    TransportLabels,
    TransportCloseLabels,
    Scopes
};

pub(super) type OpenScopes = Scopes<TransportLabels, OpenMetrics>;

#[derive(Debug, Default)]
pub(super) struct OpenMetrics {
    open_total: Counter,
    open_connections: Gauge,
    write_bytes_total: Counter,
    read_bytes_total: Counter,
}

pub(super) type CloseScopes = Scopes<TransportCloseLabels, CloseMetrics>;

#[derive(Debug, Default)]
pub(super) struct CloseMetrics {
    close_total: Counter,
    connection_duration: Histogram<latency::Ms>,
}

// ===== impl OpenScopes =====

impl OpenScopes {
    metrics! {
        tcp_open_total: Counter { "Total count of opened connections" },
        tcp_open_connections: Gauge { "Number of currently-open connections" },
        tcp_read_bytes_total: Counter { "Total count of bytes read from peers" },
        tcp_write_bytes_total: Counter { "Total count of bytes written to peers" }
    }
}

impl fmt::Display for OpenScopes {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        if self.scopes.is_empty() {
            return Ok(());
        }

        Self::tcp_open_total.fmt_help(f)?;
        Self::tcp_open_total.fmt_scopes(f, &self, |s| &s.open_total)?;

        Self::tcp_open_connections.fmt_help(f)?;
        Self::tcp_open_connections.fmt_scopes(f, &self, |s| &s.open_connections)?;

        Self::tcp_read_bytes_total.fmt_help(f)?;
        Self::tcp_read_bytes_total.fmt_scopes(f, &self, |s| &s.read_bytes_total)?;

        Self::tcp_write_bytes_total.fmt_help(f)?;
        Self::tcp_write_bytes_total.fmt_scopes(f, &self, |s| &s.write_bytes_total)?;

        Ok(())
    }
}

// ===== impl OpenMetrics =====

impl OpenMetrics {
    pub(super) fn open(&mut self) {
        self.open_total.incr();
        self.open_connections.incr();
    }

    pub(super) fn close(&mut self, rx: u64, tx: u64) {
        self.open_connections.decr();
        self.read_bytes_total += rx;
        self.write_bytes_total += tx;
    }
}

// ===== impl CloseScopes =====

impl CloseScopes {
    metrics! {
        tcp_close_total: Counter { "Total count of closed connections" },
        tcp_connection_duration_ms: Histogram<latency::Ms> { "Connection lifetimes" }
    }
}

impl fmt::Display for CloseScopes {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        if self.scopes.is_empty() {
            return Ok(());
        }

        Self::tcp_close_total.fmt_help(f)?;
        Self::tcp_close_total.fmt_scopes(f, &self, |s| &s.close_total)?;

        Self::tcp_connection_duration_ms.fmt_help(f)?;
        Self::tcp_connection_duration_ms.fmt_scopes(f, &self, |s| &s.connection_duration)?;

        Ok(())
    }
}

// ===== impl CloseMetrics =====

impl CloseMetrics {
    pub(super) fn close(&mut self, duration: Duration) {
        self.close_total.incr();
        self.connection_duration.add(duration);
    }
}
