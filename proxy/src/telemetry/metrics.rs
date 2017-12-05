use std::{u32, u64};
use std::net;
use std::sync::Arc;
use std::time::Duration;

use http;
use ordermap::OrderMap;

use control::pb::common::{HttpMethod, TcpAddress};
use control::pb::proxy::telemetry::{
    eos_ctx,
    ClientTransport,
    EosCtx,
    EosScope,
    Latency as PbLatency,
    ReportRequest,
    RequestCtx,
    RequestScope,
    ResponseCtx,
    ResponseScope,
    ServerTransport,
    StreamSummary,
    TransportSummary,
};
use ctx;
use telemetry::event::Event;

#[derive(Debug)]
pub struct Metrics {
    sources: OrderMap<net::IpAddr, TransportStats>,
    destinations: OrderMap<net::SocketAddr, TransportStats>,
    requests: OrderMap<RequestKey, RequestStats>,
    process_ctx: Arc<ctx::Process>,
}

#[derive(Debug, Eq, PartialEq, Hash)]
struct RequestKey {
    source: net::IpAddr,
    destination: net::SocketAddr,
    uri: http::Uri,
    method: http::Method,
}

#[derive(Debug, Default)]
struct RequestStats {
    count: u32,
    responses: OrderMap<Option<http::StatusCode>, ResponseStats>,
}

/// A latency in tenths of a millisecond.
#[derive(Debug, Default, Eq, PartialEq, Ord, PartialOrd, Copy, Clone, Hash)]
struct Latency(pub u32);

/// A series of latency values and counts.
#[derive(Debug, Default)]
struct Latencies(pub OrderMap<Latency, u32>);

#[derive(Debug, Default)]
struct ResponseStats {
    ends: OrderMap<End, Vec<EndStats>>,
    /// Response latencies in tenths of a millisecond.
    ///
    /// Observed latencies are mapped to a count of the times that
    /// latency value was seen.
    latencies: Latencies,
}

#[derive(Debug)]
struct EndStats {
    duration_ms: u64,
    bytes_sent: u64,
    frames_sent: u32,
}

#[derive(Debug, PartialEq, Eq, Hash)]
enum End {
    Grpc(u32),
    Reset(u32),
    Other,
}

#[derive(Debug, Default)]
struct TransportStats {
    connects: u32,
    disconnects: Vec<TransportSummary>,
}

impl RequestKey {
    fn from_ctx(ctx: &Arc<ctx::http::Request>) -> Self {
        Self {
            source: ctx.server.remote.ip(),
            destination: ctx.client.remote,
            uri: ctx.uri.clone(),
            method: ctx.method.clone(),
        }
    }
}

impl Metrics {
    pub fn new(process_ctx: Arc<ctx::Process>) -> Self {
        Metrics {
            sources: OrderMap::new(),
            destinations: OrderMap::new(),
            requests: OrderMap::new(),
            process_ctx,
        }
    }

    pub(super) fn record_event(&mut self, event: &Event) {
        match *event {
            Event::TransportOpen(ref transport) => {
                self.transport(transport).connects += 1;
            }
            Event::TransportClose(ref transport, ref close) => {
                self.transport(transport)
                    .disconnects
                    .push(TransportSummary {
                        duration_ms: dur_to_ms(close.duration),
                        bytes_sent: 0,
                    });
            }

            Event::StreamRequestOpen(ref req) => {
                self.request(req).count += 1;
            }
            Event::StreamRequestFail(ref req, ref fail) => {
                let stats = self.request(req)
                    .responses
                    .entry(None)
                    .or_insert_with(Default::default);

                let ends = stats
                    .ends
                    .entry(End::Reset(fail.error.into()))
                    .or_insert_with(Default::default);

                stats.latencies.add(fail.since_request_open);
                ends.push(EndStats {
                    // We never got a response, but we need to a count
                    // for this request + end, so a 0 EndStats is used.
                    //
                    // TODO: would be better if this case didn't need
                    // a `Vec`, and could just be a usize counter.
                    duration_ms: 0,
                    bytes_sent: 0,
                    frames_sent: 0,
                });
            }

            Event::StreamResponseOpen(ref res, ref open) => {
                self.response(res).latencies.add(open.since_request_open);
            }
            Event::StreamResponseFail(ref res, ref fail) => {
                self.response_end(res, End::Reset(fail.error.into()))
                    .push(EndStats {
                        duration_ms: dur_to_ms(fail.since_response_open),
                        bytes_sent: fail.bytes_sent,
                        frames_sent: fail.frames_sent,
                    });
            }
            Event::StreamResponseEnd(ref res, ref end) => {
                let e = end.grpc_status.map(End::Grpc).unwrap_or(End::Other);
                self.response_end(res, e).push(EndStats {
                    duration_ms: dur_to_ms(end.since_response_open),
                    bytes_sent: end.bytes_sent,
                    frames_sent: end.frames_sent,
                });
            }
        }
    }

    fn request<'a>(&mut self, req: &'a Arc<ctx::http::Request>) -> &mut RequestStats {
        self.requests
            .entry(RequestKey::from_ctx(req))
            .or_insert_with(RequestStats::default)
    }

    fn response<'a>(&mut self, res: &'a Arc<ctx::http::Response>) -> &mut ResponseStats {
        let req = self.request(&res.request);
        req.responses
            .entry(Some(res.status))
            .or_insert_with(Default::default)
    }

    fn response_end<'a>(
        &mut self,
        res: &'a Arc<ctx::http::Response>,
        end: End,
    ) -> &mut Vec<EndStats> {
        self.response(res)
            .ends
            .entry(end)
            .or_insert_with(Default::default)
    }

    fn transport<'a>(&mut self, transport: &'a ctx::transport::Ctx) -> &mut TransportStats {
        match *transport {
            ctx::transport::Ctx::Server(ref s) => {
                let source = s.remote.ip();
                self.sources
                    .entry(source)
                    .or_insert_with(TransportStats::default)
            }
            ctx::transport::Ctx::Client(ref c) => self.destinations
                .entry(c.remote)
                .or_insert_with(TransportStats::default),
        }
    }

    pub fn generate_report(&mut self) -> ReportRequest {
        let mut server_transports = Vec::new();
        let mut client_transports = Vec::new();

        for (ip, stats) in self.sources.drain(..) {
            server_transports.push(ServerTransport {
                source_ip: Some(ip.into()),
                connects: stats.connects,
                disconnects: stats.disconnects,
            })
        }

        for (addr, stats) in self.destinations.drain(..) {
            client_transports.push(ClientTransport {
                target_addr: Some(TcpAddress {
                    ip: Some(addr.ip().into()),
                    port: u32::from(addr.port()),
                }),
                connects: stats.connects,
                disconnects: stats.disconnects,
            });
        }

        let mut requests = Vec::with_capacity(self.requests.len());

        for (req, stats) in self.requests.drain(..) {
            let mut responses = Vec::with_capacity(stats.responses.len());

            for (status_code, res_stats) in stats.responses {
                let mut ends = Vec::with_capacity(res_stats.ends.len());

                for (end, end_stats) in res_stats.ends {
                    let mut streams = Vec::with_capacity(end_stats.len());

                    for stats in end_stats {
                        streams.push(StreamSummary {
                            duration_ms: stats.duration_ms,
                            bytes_sent: stats.bytes_sent,
                            frames_sent: stats.frames_sent,
                        });
                    }

                    ends.push(EosScope {
                        ctx: Some(EosCtx {
                            end: Some(match end {
                                End::Grpc(grpc) => eos_ctx::End::GrpcStatusCode(grpc),
                                End::Reset(reset) => eos_ctx::End::ResetErrorCode(reset),
                                End::Other => eos_ctx::End::Other(true),
                            }),
                        }),
                        streams,
                    });
                }


                responses.push(ResponseScope {
                    ctx: status_code.map(|code| {
                        ResponseCtx {
                            http_status_code: u32::from(code.as_u16()),
                        }
                    }),
                    ends: ends,
                    response_latencies: res_stats.latencies.into(),
                });
            }

            requests.push(RequestScope {
                ctx: Some(RequestCtx {
                    method: Some(HttpMethod::from(&req.method)),
                    path: req.uri.path().to_string(),
                    authority: req.uri
                        .authority_part()
                        .map(|a| a.to_string())
                        .unwrap_or_else(String::new),
                    source_ip: Some(req.source.into()),
                    target_addr: Some(TcpAddress {
                        ip: Some(req.destination.ip().into()),
                        port: u32::from(req.destination.port()),
                    }),
                }),
                count: stats.count,
                responses,
            })
        }

        ReportRequest {
            process: Some(self.process_ctx.as_ref().into()),
            //TODO: store proxy in Metrics?
            proxy: 0,
            server_transports,
            client_transports,
            requests,
        }
    }
}

// ===== impl Latency =====

const MS_TO_NS: u32 = 1_000_000;

impl From<Duration> for Latency {
    fn from(dur: Duration) -> Self {
        // TODO: represent ms conversion at type level...
        let as_ms = dur_to_ms(dur);

        // checked conversion to u32.
        let as_ms = if as_ms > u64::from(u32::MAX) {
            None
        } else {
            Some(as_ms as u32)
        };

        // divide the duration as ms by ten to get the value in tenths of a ms.
        let as_tenths = as_ms.and_then(|ms| ms.checked_div(10)).unwrap_or_else(|| {
            debug!("{:?} too large to convert to tenths of a millisecond!", dur);
            u32::MAX
        });

        Latency(as_tenths)
    }
}


// ===== impl Latencies =====

impl Latencies {
    #[inline]
    fn add<L: Into<Latency>>(&mut self, latency: L) {
        let value = self.0.entry(latency.into()).or_insert(0);
        *value += 1;
    }
}

impl Into<Vec<PbLatency>> for Latencies {
    fn into(mut self) -> Vec<PbLatency> {
        // NOTE: `OrderMap.drain` means we can reuse the allocated memory --- can we
        //      ensure we're not allocating a new OrderMap after covnerting to pb?
        self.0
            .drain(..)
            .map(|(Latency(latency), count)| {
                PbLatency {
                    latency,
                    count,
                }
            })
            .collect()
    }
}

fn dur_to_ms(dur: Duration) -> u64 {
    dur.as_secs()
        // note that this could just be saturating addition if we didn't want
        // to log if an overflow occurs...
        .checked_mul(1_000)
        .and_then(|as_millis| {
            let subsec = u64::from(dur.subsec_nanos() / MS_TO_NS);
            as_millis.checked_add(subsec)
        })
        .unwrap_or_else(|| {
            debug!("{:?} too large to convert to ms!", dur);
            u64::MAX
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn latencies_incr() {
        let mut latencies = Latencies::default();
        assert!(latencies.0.is_empty());

        latencies.add(Duration::from_secs(10));
        assert_eq!(
            latencies.0.get(&Latency::from(Duration::from_secs(10))),
            Some(&1)
        );

        latencies.add(Duration::from_secs(15));
        assert_eq!(
            latencies.0.get(&Latency::from(Duration::from_secs(10))),
            Some(&1)
        );
        assert_eq!(
            latencies.0.get(&Latency::from(Duration::from_secs(15))),
            Some(&1)
        );

        latencies.add(Duration::from_secs(10));
        assert_eq!(
            latencies.0.get(&Latency::from(Duration::from_secs(10))),
            Some(&2)
        );
        assert_eq!(
            latencies.0.get(&Latency::from(Duration::from_secs(15))),
            Some(&1)
        );
    }
}
