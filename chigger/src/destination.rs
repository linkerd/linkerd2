//! A gRPC client for the Destination API.
//!
//! This client currently discovers a destination controller pod via the k8s API and uses port
//! forwarding to connect to a running instance.

use anyhow::Result;
use k8s_openapi::api::core::v1 as corev1;
use kube::api::ResourceExt;
use linkerd2_proxy_api::destination as api;
use std::sync::Arc;
use tokio::{io, sync::Mutex};

#[derive(Clone, Debug)]
pub struct DestinationClient {
    client: Arc<Mutex<api::destination_client::DestinationClient<GrpcHttp>>>,
    pod: corev1::Pod,
}

#[derive(Debug)]
struct GrpcHttp {
    tx: hyper::client::conn::SendRequest<tonic::body::BoxBody>,
}

async fn get_policy_controller_pod(client: &kube::Client) -> Result<corev1::Pod> {
    let params =
        kube::api::ListParams::default().labels("linkerd.io/control-plane-component=destination");
    let mut pods = kube::Api::<corev1::Pod>::namespaced(client.clone(), "linkerd")
        .list(&params)
        .await?;
    let pod = pods
        .items
        .pop()
        .ok_or_else(|| anyhow::anyhow!("no destination controller pods found"))?;
    Ok(pod)
}

async fn connect_port_forward(
    client: &kube::Client,
    pod: &str,
) -> Result<impl io::AsyncRead + io::AsyncWrite + Unpin> {
    let mut pf = kube::Api::<corev1::Pod>::namespaced(client.clone(), "linkerd")
        .portforward(pod, &[8086])
        .await?;
    let io = pf.take_stream(8086).expect("must have a stream");
    Ok(io)
}

// === impl DestinationClient ===

impl DestinationClient {
    pub async fn port_forwarded(client: &kube::Client) -> Self {
        let pod = get_policy_controller_pod(client)
            .await
            .expect("failed to find a policy controller pod");
        let io = connect_port_forward(client, &pod.name_unchecked())
            .await
            .expect("failed to establish a port forward");
        let http = GrpcHttp::handshake(io)
            .await
            .expect("failed to connect to the gRPC server");
        Self {
            client: Arc::new(Mutex::new(api::destination_client::DestinationClient::new(
                http,
            ))),
            pod,
        }
    }

    pub async fn watch(
        &mut self,
        svc: &corev1::Service,
        port: u16,
    ) -> Result<tonic::Streaming<api::Update>, tonic::Status> {
        let name = svc
            .metadata
            .name
            .as_ref()
            .expect("Service must have a cluster ip");
        let ns = svc
            .metadata
            .namespace
            .clone()
            .unwrap_or_else(|| "default".to_string());
        let node_name = self.pod.spec.as_ref().unwrap().node_name.as_ref().unwrap();
        let mut client = self.client.lock().await;
        let rsp = client
            .get(tonic::Request::new(api::GetDestination {
                path: format!("{name}.{ns}.svc.cluster.local:{port}"),
                context_token: format!("{{\"ns\":\"{ns}\",\"nodeName\":\"{node_name}\"}}"),
                scheme: Default::default(),
            }))
            .await?;
        Ok(rsp.into_inner())
    }
}

// === impl GrpcHttp ===

impl GrpcHttp {
    async fn handshake<I>(io: I) -> Result<Self>
    where
        I: io::AsyncRead + io::AsyncWrite + Unpin + Send + 'static,
    {
        let (tx, conn) = hyper::client::conn::Builder::new()
            .http2_only(true)
            .handshake(io)
            .await?;
        tokio::spawn(conn);
        Ok(Self { tx })
    }
}

impl hyper::service::Service<hyper::Request<tonic::body::BoxBody>> for GrpcHttp {
    type Response = hyper::Response<hyper::Body>;
    type Error = hyper::Error;
    type Future = hyper::client::conn::ResponseFuture;

    fn poll_ready(
        &mut self,
        cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Result<(), Self::Error>> {
        self.tx.poll_ready(cx)
    }

    fn call(&mut self, req: hyper::Request<tonic::body::BoxBody>) -> Self::Future {
        let (mut parts, body) = req.into_parts();

        let mut uri = parts.uri.into_parts();
        uri.scheme = Some(hyper::http::uri::Scheme::HTTP);
        uri.authority = Some(
            "linkerd-destination.linkerd.svc.cluster.local:8090"
                .parse()
                .unwrap(),
        );
        parts.uri = hyper::Uri::from_parts(uri).unwrap();

        self.tx.call(hyper::Request::from_parts(parts, body))
    }
}
