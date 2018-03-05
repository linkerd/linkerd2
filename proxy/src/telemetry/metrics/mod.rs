use std::net;
use std::sync::Arc;
use std::time::Duration;
use std::{u32, u64};

use http;
use indexmap::IndexMap;

use conduit_proxy_controller_grpc::common::{
    TcpAddress,
    Protocol,
};
use conduit_proxy_controller_grpc::telemetry::{
    ClientTransport,
    eos_ctx,
    EosCtx,
    EosScope,
    ReportRequest,
    RequestCtx,
    RequestScope,
    ResponseCtx,
    ResponseScope,
    ServerTransport,
    TransportSummary,
};
use ctx;
use telemetry::event::Event;

mod latency;

#[derive(Debug)]
pub struct Metrics {
    sources: IndexMap<net::IpAddr, TransportStats>,
    destinations: IndexMap<net::SocketAddr, TransportStats>,
    requests: IndexMap<RequestKey, RequestStats>,
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
    responses: IndexMap<Option<http::StatusCode>, ResponseStats>,
}

#[derive(Debug, Default)]
struct ResponseStats {
    ends: IndexMap<End, u32>,
    /// Response latencies in tenths of a millisecond.
    ///
    /// Observed latencies are mapped to a count of the times that
    /// latency value was seen.
    latencies: latency::Histogram,
}

#[derive(Debug, PartialEq, Eq, Hash)]
enum End {
    Grpc(u32),
    Reset(u32),
    Other,
}

#[derive(Debug, Default)]
struct TransportStats {
    protocol: Protocol,
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
            sources: IndexMap::new(),
            destinations: IndexMap::new(),
            requests: IndexMap::new(),
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

                stats.latencies += fail.since_request_open;
                *ends += 1;
            }

            Event::StreamResponseOpen(ref res, ref open) => {
                self.response(res).latencies += open.since_request_open;
            },
            Event::StreamResponseFail(ref res, ref fail) => {
                *self.response_end(res, End::Reset(fail.error.into()))
                    += 1;
            }
            Event::StreamResponseEnd(ref res, ref end) => {
                let e = end.grpc_status.map(End::Grpc).unwrap_or(End::Other);
                *self.response_end(res, e) += 1;
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
    ) -> &mut u32 {
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
                    .or_insert_with(|| TransportStats {
                        protocol: s.protocol,
                        ..TransportStats::default()
                    })
            }
            ctx::transport::Ctx::Client(ref c) => self.destinations
                .entry(c.remote)
                .or_insert_with(|| TransportStats {
                    protocol: c.protocol,
                    ..TransportStats::default()
                })
        }
    }

    pub fn generate_report(&mut self) -> ReportRequest {
        let histogram_bucket_bounds_tenth_ms: Vec<u32> =
            latency::BUCKET_BOUNDS.iter()
                .map(|&latency| latency.into())
                .collect();

        let mut server_transports = Vec::new();
        let mut client_transports = Vec::new();

        for (ip, stats) in self.sources.drain(..) {
            server_transports.push(ServerTransport {
                source_ip: Some(ip.into()),
                connects: stats.connects,
                disconnects: stats.disconnects,
                protocol: stats.protocol as i32,
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
                protocol: stats.protocol as i32,
            });
        }

        let mut requests = Vec::with_capacity(self.requests.len());

        for (req, stats) in self.requests.drain(..) {
            let mut responses = Vec::with_capacity(stats.responses.len());

            for (status_code, res_stats) in stats.responses {
                let mut ends = Vec::with_capacity(res_stats.ends.len());

                for (end, streams) in res_stats.ends {

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
                    response_latency_counts: res_stats.latencies
                        .into_iter()
                        .map(|l| *l)
                        .collect(),
                });
            }

            requests.push(RequestScope {
                ctx: Some(RequestCtx {
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
            histogram_bucket_bounds_tenth_ms,
        }
    }
}

fn dur_to_ms(dur: Duration) -> u64 {
    dur.as_secs()
        // note that this could just be saturating addition if we didn't want
        // to log if an overflow occurs...
        .checked_mul(1_000)
        .and_then(|as_millis| {
            let subsec = u64::from(dur.subsec_nanos() / latency::MS_TO_NS);
            as_millis.checked_add(subsec)
        })
        .unwrap_or_else(|| {
            debug!("{:?} too large to convert to ms!", dur);
            u64::MAX
        })
}
