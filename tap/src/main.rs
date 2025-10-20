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
use std::time::Duration;
use tokio::select;
use tokio::sync::broadcast::error::RecvError;
use tokio::sync::{broadcast, mpsc, watch};
use tokio_stream::wrappers::{ReceiverStream, WatchStream};
use tokio_stream::StreamExt;
use tokio_util::sync::CancellationToken;
use tonic::codec::CompressionEncoding;
use tonic::codegen::BoxStream;
use tonic::transport::Channel;
use tonic::{Request, Response, Status};
use tracing::{debug, info};
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        // .with_env_filter(EnvFilter::new("debug"))
        .with_env_filter(EnvFilter::new("info"))
        .init();

    let (span_tx, _) = broadcast::channel(100);
    let (match_tx, mut match_rx) = mpsc::channel::<MatchBatch>(1);

    {
        let tx = span_tx.clone();
        tokio::spawn(
            tonic::transport::Server::builder()
                .add_service(
                    TraceServiceServer::new(TraceCollector { tx })
                        .send_compressed(CompressionEncoding::Gzip)
                        .accept_compressed(CompressionEncoding::Gzip),
                )
                .serve("0.0.0.0:4317".parse().expect("Must parse correctly")),
        );
    }

    {
        tokio::spawn(
            tonic::transport::Server::builder()
                .add_service(InstrumentServer::new(InstrumentHandler {
                    tx: span_tx,
                    match_tx,
                }))
                .serve("0.0.0.0:8080".parse().expect("Must parse correctly")),
        );
    }

    let mut pods = HashMap::<String, MatchStream>::new();
    while let Some(batch) = match_rx.recv().await {
        // info!(%batch, "");
        if let Some(pod) = pods.get_mut(&batch.pod) {
            if let Some(matches) = batch.matches {
                if let Some(existing_match) = pod.requests.get_mut(&batch.req_id) {
                    info!(id = batch.req_id, "Updating existing match");
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
                    info!(id = batch.req_id, "Inserting new match");
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
                info!(
                    pod = ?batch.pod,
                    request = batch.req_id,
                    "Removing pod connection"
                );
                pod.requests.remove(&batch.req_id);
                if pod.requests.is_empty() {
                    pods.remove(&batch.pod);
                }
            }
        } else if let Some(matches) = batch.matches {
            let (tx, rx) = watch::channel(vec![TraceMatch {
                id: batch.req_id.clone(),
                r#match: Some(observe_request::Match {
                    r#match: Some(matches.clone()),
                }),
            }]);
            pods.insert(
                batch.pod.clone(),
                MatchStream {
                    requests: HashMap::from_iter([(batch.req_id.clone(), matches.clone())]),
                    stream: tx,
                },
            );

            tokio::spawn(async move {
                let channel = Channel::from_static("http://127.0.0.1:4190").connect_lazy();
                let mut client = TapClient::new(channel);
                info!("Connecting to pod");
                let a = client
                    .observe_trace(WatchStream::new(rx).map(|matches| {
                        info!(?matches, "Sending tap update");
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
                match a {
                    Ok(resp) => {
                        let resp = resp.into_inner();
                        info!(
                            pod = ?batch.pod,
                            request = batch.req_id,
                            ?resp,
                            "Pod connection complete"
                        );
                    }
                    Err(e) => {
                        info!(
                            pod = ?batch.pod,
                            request = batch.req_id,
                            status=%e,
                            "Pod connection failed"
                        );
                    }
                }
            });
        } else {
            info!(request = batch.req_id, "Ignoring empty request");
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
        // for span in &request.resource_spans {
        //     // debug!("resource attrs: {:?}", span.resource.as_ref().map(|r| &r.attributes));
        //     for span in &span.scope_spans {
        //         // debug!("span scope attrs: {:?}", span.scope.as_ref().map(|s| &s.attributes));
        //         for span in &span.spans {
        //             debug!("span attrs: {:?}", span.attributes);
        //         }
        //     }
        // }
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
}

#[derive(Debug)]
struct MatchBatch {
    pod: String,
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
        info!(?request, "Got tap request");
        let Some(matches) = request.r#match else {
            todo!()
        };
        if self
            .match_tx
            .send(MatchBatch {
                pod: "web".to_string(),
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
            let request_id = request.id.clone();
            let match_tx = self.match_tx.clone();
            tokio::spawn(async move {
                loop {
                    let res = select! {
                        res = rx.recv() => res,
                        _ = token.cancelled() => break,
                        _ = client_tx.closed() => break,
                    };

                    let mut spans = match res {
                        Ok(v) => v,
                        Err(RecvError::Closed) => break,
                        Err(RecvError::Lagged(missed)) => {
                            eprintln!("Dropped {missed} traces");
                            continue;
                        }
                    };

                    info!("Received {} spans", spans.scope_spans.iter().flat_map(|s| &s.spans).count());

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
                                value.split(',').any(|id| id == request_id)
                            })
                        });

                        !spans.spans.is_empty()
                    });
                    if spans.scope_spans.is_empty() {
                        info!("Ignoring all spans");
                        continue;
                    }

                    info!("Sending {} spans", spans.scope_spans.iter().flat_map(|s| &s.spans).count());

                    if client_tx
                        .send(Ok(WatchResposne {
                            kind: Some(Kind::Spans(spans.encode_to_vec())),
                        }))
                        .await
                        .is_err()
                    {
                        token.cancel();
                        break;
                    }
                }

                token.cancel();
                info!(%request_id, "Closing client connection");
                let _ = match_tx
                    .send(MatchBatch {
                        pod: "web".to_string(),
                        req_id: request_id,
                        matches: None,
                    })
                    .await;
            });
        }

        let token = token.clone();

        let stream = ReceiverStream::new(client_rx).chain(
            tokio_stream::once(Ok(WatchResposne::default())).filter(move |_| {
                token.cancel();
                false
            }),
        );

        Ok(Response::new(Box::pin(stream)))
    }
}
