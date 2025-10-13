use async_trait::async_trait;
use linkerd2_proxy_api::tap::{
    instrument_server::InstrumentServer,
    observe_request,
    observe_request::r#match::{Match, Seq},
    tap_client::TapClient,
    ObserveTraceRequest, WatchRequest, WatchResposne,
};
use opentelemetry_proto::tonic::collector::trace::v1::trace_service_server::{
    TraceService, TraceServiceServer,
};
use opentelemetry_proto::tonic::collector::trace::v1::{
    ExportTraceServiceRequest, ExportTraceServiceResponse,
};
use opentelemetry_proto::tonic::trace::v1::ResourceSpans;
use prost::Message;
use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;
use tokio::select;
use tokio::sync::broadcast::error::RecvError;
use tokio::sync::{broadcast, mpsc};
use tokio_stream::wrappers::ReceiverStream;
use tokio_stream::StreamExt;
use tokio_util::sync::CancellationToken;
use tonic::codegen::BoxStream;
use tonic::transport::Channel;
use tonic::{Request, Response, Status};
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt().with_env_filter(EnvFilter::new("debug")).init();

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
                .add_service(InstrumentServer::new(TraceReceiver {
                    tx: span_tx,
                    match_tx,
                    next_req_id: AtomicU64::new(0),
                }))
                .serve("0.0.0.0:8080".parse().expect("Must parse correctly")),
        );
    }

    let mut clients = HashMap::<(), MatchStream>::new();
    while let Some(batch) = match_rx.recv().await {
        if let Some(client) = clients.get_mut(&batch.client) {
            if let Some(matches) = batch.matches {
                if let Some(existing_match) = client.requests.get_mut(&batch.req_id) {
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
                    client.requests.insert(batch.req_id, matches.clone());
                }
                let new_match = Match::All(Seq {
                    matches: client
                        .requests
                        .iter()
                        .map(|(_, m)| observe_request::Match {
                            r#match: Some(m.clone()),
                        })
                        .collect(),
                });
                if client.stream.send(new_match).await.is_err() {
                    clients.remove(&batch.client);
                }
            } else {
                client.requests.remove(&batch.req_id);
                if client.requests.is_empty() {
                    clients.remove(&batch.client);
                }
            }
        } else {
            if let Some(matches) = batch.matches {
                let (tx, rx) = mpsc::channel(1);
                tx.try_send(matches.clone())
                    .expect("First send must succeed");
                clients.insert(
                    batch.client,
                    MatchStream {
                        requests: HashMap::from_iter([(batch.req_id, matches.clone())]),
                        stream: tx,
                    },
                );

                tokio::spawn(async move {
                    let channel = match Channel::from_static("127.0.0.1:4190").connect().await {
                        Ok(c) => c,
                        Err(_) => return,
                    };
                    let mut client = TapClient::new(channel);
                    let _ = client
                        .observe_trace(ReceiverStream::new(rx).map(|matches| {
                            ObserveTraceRequest {
                                sample_percent: Some(1.0),
                                max_samples_per_second: None,
                                report_interval: Some(
                                    prost_types::Duration::try_from(Duration::from_secs(1))
                                        .expect("must convert"),
                                ),
                                r#match: Some(observe_request::Match {
                                    r#match: Some(matches),
                                }),
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

struct TraceReceiver {
    tx: broadcast::Sender<ResourceSpans>,
    match_tx: mpsc::Sender<MatchBatch>,
    next_req_id: AtomicU64,
}

struct MatchBatch {
    client: (),
    req_id: u64,
    matches: Option<Match>,
}

struct MatchStream {
    requests: HashMap<u64, Match>,
    stream: mpsc::Sender<Match>,
}

#[async_trait]
impl linkerd2_proxy_api::tap::instrument_server::Instrument for TraceReceiver {
    type WatchStream = BoxStream<WatchResposne>;

    async fn watch(
        &self,
        request: Request<WatchRequest>,
    ) -> Result<Response<Self::WatchStream>, Status> {
        let mut rx = self.tx.subscribe();
        let (client_tx, client_rx) = mpsc::channel(100);

        let token = CancellationToken::new();

        let Some(matches) = request.into_inner().r#match else {
            todo!()
        };
        let req_id = self.next_req_id.fetch_add(1, Ordering::Relaxed);
        if let Err(e) = self
            .match_tx
            .send(MatchBatch {
                client: (),
                req_id,
                matches: Some(matches.r#match.unwrap()),
            })
            .await
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

                    let spans = match res {
                        Ok(v) => v,
                        Err(RecvError::Closed) => break,
                        Err(RecvError::Lagged(missed)) => {
                            eprintln!("Dropped {missed} traces");
                            continue;
                        }
                    };

                    if let Err(_) = client_tx
                        .send(Ok(WatchResposne {
                            traces: vec![spans.encode_to_vec()],
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
