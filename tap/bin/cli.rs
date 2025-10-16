use async_trait::async_trait;
use opentelemetry_proto::tonic::collector::trace::v1::{
    ExportTraceServiceRequest, ExportTraceServiceResponse,
};
use opentelemetry_proto::tonic::common::v1::any_value::Value;
use opentelemetry_proto::tonic::common::v1::{AnyValue, KeyValue};
use opentelemetry_proto::tonic::trace::v1::span::SpanKind;
use std::collections::BTreeMap;
use std::fmt::{Debug, Formatter};
use std::io::stdout;
use std::time::Duration;
use clap::Parser;
use owo_colors::OwoColorize;
use serde::Serialize;
use tonic::{Request, Response, Status};

#[derive(clap::Parser)]
struct Args {
    #[arg(long)]
    all: bool,
    #[arg(long)]
    json: bool,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Args::parse();



    Ok(())
}

#[derive(Copy, Clone)]
struct OtelServer {
    all: bool,
    json: bool,
}

// static POD_UID: &str = "7b845938-1c62-4f02-9403-8ad2184dd1f1";
// static POD_UID: &str = "3a3a75c5-cdbd-446a-91a4-81aafa225294";

#[async_trait]
impl opentelemetry_proto::tonic::collector::trace::v1::trace_service_server::TraceService
for OtelServer
{
    async fn export(
        &self,
        request: Request<ExportTraceServiceRequest>,
    ) -> Result<Response<ExportTraceServiceResponse>, Status> {
        for span in request.into_inner().resource_spans {
            let Some(resource) = span.resource else {
                continue;
            };
            let attrs = Attributes::new(resource.attributes);
            if attrs.get_string("k8s.container.name") != "linkerd-proxy" {
                continue;
            }
            if !self.all && attrs.get_string("k8s.pod.uid") != "7b845938-1c62-4f02-9403-8ad2184dd1f1" {
                continue;
            }

            // println!("{attrs:?}");
            for span in span.scope_spans {
                // println!("Got span group");
                for span in span.spans {
                    let kind = SpanKind::try_from(span.kind).unwrap_or(SpanKind::Unspecified);
                    if kind != SpanKind::Client {
                        continue;
                    }

                    let span_attrs = Attributes::new(span.attributes);
                    let latency =
                        Duration::from_nanos(span.end_time_unix_nano - span.start_time_unix_nano);
                    let entry = Entry {
                        latency,
                        pod: attrs.get_string("host.name"),
                        transport: span_attrs.try_get_string("network.transport"),
                        proto: span_attrs.try_get_string("http.request.header.l5d-orig-proto"),
                        direction: span_attrs.get_string("direction"),
                        method: span_attrs.get_string("http.request.method"),
                        url: span_attrs.get_string("url.full"),
                        content_length: span_attrs.try_get_string("http.request.header.content-length"),
                        status: span_attrs.get_string("http.response.status_code"),
                        user_agent: span_attrs.get_string("user_agent.original"),
                        attrs: &attrs,
                    };
                    if self.json {
                        let _ = serde_json::to_writer(stdout(), &entry);
                        println!();
                    } else {
                        println!("{entry:?}");
                    }
                    // println!(
                    //     // "{}: {}: {}, latency={:?}, kind={:?}, direction={}, {} {}://{}:{}{}?{} -> {}, attrs={:?}",
                    //     // "latency={:?}, pod={:?}, kind={:?}, proto={};{}, direction={}, {} {} ({}B) -> {}, user_agent={}",
                    //     "latency={:?}, pod={:?}, proto={}, direction={}, {} {} ({}B) -> {}, user_agent={}",
                    //     // BASE64.encode(&span.parent_span_id),
                    //     // BASE64.encode(&span.trace_id),
                    //     // BASE64.encode(&span.span_id),
                    //     latency,
                    //     attrs.get_string("host.name"),
                    //     // kind,
                    //     // span_attrs.get_string("network.transport"),
                    //     span_attrs.get_string("http.request.header.l5d-orig-proto"),
                    //     span_attrs.get_string("direction"),
                    //     span_attrs.get_string("http.request.method"),
                    //     span_attrs.get_string("url.full"),
                    //     span_attrs.get_string_or("http.request.header.content-length", "0"),
                    //     span_attrs.get_string("http.response.status_code"),
                    //     span_attrs.get_string("user_agent.original"),
                    //     // span_attrs,
                    // );
                }
            }
        }
        Ok(Response::new(ExportTraceServiceResponse {
            partial_success: None,
        }))
    }
}

#[derive(Serialize)]
struct Entry<'a> {
    latency: Duration,
    pod: &'a str,
    transport: Option<&'a str>,
    proto: Option<&'a str>,
    direction: &'a str,
    method: &'a str,
    url: &'a str,
    content_length: Option<&'a str>,
    status: &'a str,
    user_agent: &'a str,
    attrs: &'a Attributes,
}

impl Debug for Entry<'_> {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "latency={:?}, pod={}, ", self.latency, self.pod)?;
        match (&self.transport, &self.proto) {
            (Some(t), Some(p)) => write!(f, "proto={t};{p}, ")?,
            (Some(t), None) => write!(f, "proto={t}, ")?,
            (None, Some(p)) => write!(f, "proto={p}, ")?,
            (None, None) => {},
        }
        write!(f, "dir={}, {} {} ", self.direction, self.method, self.url)?;
        if let Some(c) = &self.content_length {
            write!(f, "({c}B) ")?;
        }
        if let Ok(status) = self.status.parse::<u16>() {
            if status < 400 {
                write!(f, "-> {}, ", status.green())?;
            } else {
                write!(f, "-> {}, ", status.red())?;
            }
        } else {
            write!(f, "-> {}, ", self.status)?;
        }
        write!(f, "ua={}", self.user_agent)?;
        Ok(())
    }
}

#[derive(Serialize)]
struct Attributes {
    #[serde(flatten)]
    inner: BTreeMap<String, AnyValue>,
}

impl Attributes {
    fn new(values: Vec<KeyValue>) -> Self {
        Self {
            inner: BTreeMap::from_iter(values
                .into_iter()
                .filter_map(|kv| kv.value.map(|v| (kv.key, v))))
        }
    }

    fn try_get_string(&self, key: &str) -> Option<&str> {
        let AnyValue {
            value: Some(Value::StringValue(value)),
        } = self.inner.get(key)?
        else {
            return None;
        };
        Some(value)
    }

    fn get_string(&self, key: &str) -> &str {
        self.get_string_or(key, "")
    }

    fn get_string_or(&self, key: &str, default: &'static str) -> &str {
        self.try_get_string(key).unwrap_or(default)
    }
}

impl Debug for Attributes {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        let mut m = f.debug_map();
        for (key, value) in &self.inner {
            if let Some(value) = &value.value {
                m.key(key);
                match value {
                    Value::StringValue(v) => m.value(v),
                    Value::BoolValue(v) => m.value(v),
                    Value::IntValue(v) => m.value(v),
                    Value::DoubleValue(v) => m.value(v),
                    Value::ArrayValue(_) => todo!(),
                    Value::KvlistValue(_) => todo!(),
                    Value::BytesValue(_) => todo!(),
                };
            }
        }
        m.finish()
    }
}
