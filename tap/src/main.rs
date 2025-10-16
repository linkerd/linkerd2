use async_trait::async_trait;
use linkerd2_proxy_api::tap::watch_resposne::Kind;
use linkerd2_proxy_api::tap::{
    instrument_server::InstrumentServer,
    observe_request,
    observe_request::r#match::{Match, Seq},
    tap_client::TapClient,
    ObserveTraceRequest, TraceMatch, WatchRequest, WatchResposne,
};
use opentelemetry_proto::tonic::collector::trace::v1::trace_service_server::{
    TraceService, TraceServiceServer,
};
use opentelemetry_proto::tonic::collector::trace::v1::{
    ExportTraceServiceRequest, ExportTraceServiceResponse,
};
use opentelemetry_proto::tonic::common::v1::{any_value, AnyValue};
use opentelemetry_proto::tonic::trace::v1::ResourceSpans;
use prost::Message;
use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;
use tokio::select;
use tokio::sync::broadcast::error::RecvError;
use tokio::sync::{broadcast, mpsc, watch};
use tokio_stream::wrappers::{ReceiverStream, WatchStream};
use tokio_stream::StreamExt;
use tokio_util::sync::CancellationToken;
use tonic::codegen::BoxStream;
use tonic::transport::Channel;
use tonic::{Request, Response, Status};
use tracing::{debug, info};
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::new("debug"))
        .init();

    let (span_tx, _) = broadcast::channel(100);
    let (match_tx, mut match_rx) = mpsc::channel::<MatchBatch>(1);

    {
        let tx = span_tx.clone();
        tokio::spawn(
            tonic::transport::Server::builder()
                .add_service(TraceServiceServer::new(TraceCollector { tx }))
                .serve("0.0.0.0:4317".parse().expect("Must parse correctly")),
        );
    }

    {
        tokio::spawn(
            tonic::transport::Server::builder()
                .add_service(InstrumentServer::new(InstrumentHandler {
                    tx: span_tx,
                    match_tx,
                    next_req_id: AtomicU64::new(0),
                }))
                .serve("0.0.0.0:8080".parse().expect("Must parse correctly")),
        );
    }

    let mut pods = HashMap::<(), MatchStream>::new();
    while let Some(batch) = match_rx.recv().await {
        info!("Match batch: {batch:?}");
        if let Some(pod) = pods.get_mut(&batch.pod) {
            if let Some(matches) = batch.matches {
                if let Some(existing_match) = pod.requests.get_mut(&batch.req_id) {
                    info!("Updating existing match for {}", batch.req_id);
                    *existing_match = Match::All(Seq {
                        matches: vec![
                            observe_request::Match {
                                r#match: Some(existing_match.clone()),
                            },
                            observe_request::Match {
                                r#match: Some(matches),
                            },
                        ],
                    });
                } else {
                    info!("Inserting new match for {}", batch.req_id);
                    pod.requests.insert(batch.req_id, matches.clone());
                }
                let new_match = pod
                    .requests
                    .iter()
                    .map(|(id, m)| TraceMatch {
                        id: id.clone(),
                        r#match: Some(observe_request::Match {
                            r#match: Some(m.clone()),
                        }),
                    })
                    .collect();
                if pod.stream.send(new_match).is_err() {
                    pods.remove(&batch.pod);
                }
            } else {
                pod.requests.remove(&batch.req_id);
                if pod.requests.is_empty() {
                    pods.remove(&batch.pod);
                }
            }
        } else {
            if let Some(matches) = batch.matches {
                let (tx, rx) = watch::channel(vec![TraceMatch {
                    id: batch.req_id.clone(),
                    r#match: Some(observe_request::Match {
                        r#match: Some(matches.clone()),
                    }),
                }]);
                pods.insert(
                    batch.pod,
                    MatchStream {
                        requests: HashMap::from_iter([(batch.req_id, matches.clone())]),
                        stream: tx,
                    },
                );

                info!("Spawning pod connection");
                tokio::spawn(async move {
                    let channel = match Channel::from_static("127.0.0.1:4190").connect().await {
                        Ok(c) => c,
                        Err(_) => return,
                    };
                    let mut client = TapClient::new(channel);
                    let _ = client
                        .observe_trace(WatchStream::new(rx).map(|matches| {
                            info!("Sending tap update: {matches:?}");
                            ObserveTraceRequest {
                                sample_percent: Some(1.0),
                                max_samples_per_second: None,
                                report_interval: Some(
                                    prost_types::Duration::try_from(Duration::from_secs(1))
                                        .expect("must convert"),
                                ),
                                matches,
                            }
                        }))
                        .await;
                });
            }
        }
    }

    Ok(())
}

struct TraceCollector {
    tx: broadcast::Sender<ResourceSpans>,
}

#[async_trait]
impl TraceService for TraceCollector {
    async fn export(
        &self,
        request: Request<ExportTraceServiceRequest>,
    ) -> Result<Response<ExportTraceServiceResponse>, Status> {
        let request = request.into_inner();
        for span in request.resource_spans {
            // Ignore errors. No receivers just means no clients are currently connected.
            let _ = self.tx.send(span);
        }

        Ok(Response::new(ExportTraceServiceResponse {
            partial_success: None,
        }))
    }
}

struct InstrumentHandler {
    tx: broadcast::Sender<ResourceSpans>,
    match_tx: mpsc::Sender<MatchBatch>,
    next_req_id: AtomicU64,
}

#[derive(Debug)]
struct MatchBatch {
    pod: (),
    req_id: String,
    matches: Option<Match>,
}

struct MatchStream {
    requests: HashMap<String, Match>,
    stream: watch::Sender<Vec<TraceMatch>>,
}

#[async_trait]
impl linkerd2_proxy_api::tap::instrument_server::Instrument for InstrumentHandler {
    type WatchStream = BoxStream<WatchResposne>;

    async fn watch(
        &self,
        request: Request<WatchRequest>,
    ) -> Result<Response<Self::WatchStream>, Status> {

        let mut rx = self.tx.subscribe();
        let (client_tx, client_rx) = mpsc::channel(100);

        let token = CancellationToken::new();

        let request = request.into_inner();
        info!("Got tap request: {request:?}");
        let Some(matches) = request.r#match else {
            todo!()
        };
        if self
            .match_tx
            .send(MatchBatch {
                pod: (),
                req_id: request.id.clone(),
                matches: Some(matches.r#match.unwrap()),
            })
            .await
            .is_err()
        {
            token.cancel();
            return Ok(Response::new(Box::pin(tokio_stream::empty())));
        };

        {
            let token = token.clone();
            tokio::spawn(async move {
                loop {
                    let res = select! {
                        res = rx.recv() => res,
                        _ = token.cancelled() => break,
                    };

                    let mut spans = match res {
                        Ok(v) => v,
                        Err(RecvError::Closed) => break,
                        Err(RecvError::Lagged(missed)) => {
                            eprintln!("Dropped {missed} traces");
                            continue;
                        }
                    };

                    spans.scope_spans.retain_mut(|spans| {
                        spans.spans.retain(|s| {
                            s.attributes.iter().any(|attr| {
                                if attr.key != "linkerd.tap.id" {
                                    return false;
                                }
                                let Some(AnyValue {
                                    value: Some(any_value::Value::StringValue(value)),
                                }) = &attr.value
                                else {
                                    return false;
                                };
                                value.split(',').any(|id| id == request.id)
                            })
                        });

                        !spans.spans.is_empty()
                    });
                    if spans.scope_spans.is_empty() {
                        continue;
                    }

                    if let Err(_) = client_tx
                        .send(Ok(WatchResposne {
                            kind: Some(Kind::Spans(spans.encode_to_vec())),
                        }))
                        .await
                    {
                        token.cancel();
                        break;
                    }
                }
            });
        }
        let stream = ReceiverStream::new(client_rx);

        Ok(Response::new(Box::pin(stream)))
    }
}
