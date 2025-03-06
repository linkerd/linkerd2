use futures::StreamExt;
use k8s_openapi::apimachinery::pkg::util::intstr::IntOrString;
use linkerd2_proxy_api::outbound::{proxy_protocol, OutboundPolicy, ProxyProtocol};
use linkerd_policy_controller_k8s_api::{self as k8s};
use linkerd_policy_test::{
    await_condition, create, create_ready_pod, curl, endpoints_ready,
    outbound_api::retry_watch_outbound_policy, test_route::TestParent, web, with_temp_ns,
    LinkerdInject,
};
use maplit::{btreemap, convert_args};

const OPAQUE_PORT: i32 = 81;
const HTTP1_PORT: i32 = 82;
const HTTP2_PORT: i32 = 83;

#[tokio::test(flavor = "current_thread")]
async fn app_protocol() {
    with_temp_ns(|client, ns| async move {
        // Create the web pod and wait for it to be ready.
        let (svc, _) = tokio::join!(
            create(&client, service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        let (opaque_config, http1_config, http2_config) = tokio::join!(
            policy(&client, &ns, svc.ip(), OPAQUE_PORT as u16),
            policy(&client, &ns, svc.ip(), HTTP1_PORT as u16),
            policy(&client, &ns, svc.ip(), HTTP2_PORT as u16),
        );
        assert!(matches!(
            opaque_config.protocol,
            Some(ProxyProtocol {
                kind: Some(proxy_protocol::Kind::Opaque(_))
            })
        ));
        assert!(matches!(
            http1_config.protocol,
            Some(ProxyProtocol {
                kind: Some(proxy_protocol::Kind::Http1(_))
            })
        ));
        assert!(matches!(
            http2_config.protocol,
            Some(ProxyProtocol {
                kind: Some(proxy_protocol::Kind::Http2(_))
            })
        ));

        let opaque_endpoint = format!("http://web:{OPAQUE_PORT}");
        let http1_endpoint = format!("http://web:{HTTP1_PORT}");
        let http2_endpoint = format!("http://web:{HTTP2_PORT}");

        let curl = curl::Runner::init(&client, &ns).await;
        let (opaque, http1, http2) = tokio::join!(
            curl.run("curl-opaque", &opaque_endpoint, LinkerdInject::Enabled),
            curl.run("curl-http1", &http1_endpoint, LinkerdInject::Enabled),
            curl.run("curl-http2", &http2_endpoint, LinkerdInject::Enabled),
        );
        let (opaque_status, http1_status, http2_exit) = tokio::join!(
            opaque.http_status_code(),
            http1.http_status_code(),
            // Server only supports HTTP/1, should result in failed exit code without a valid HTTP status
            http2.exit_code(),
        );
        assert_eq!(
            opaque_status, 204,
            "opaque request must be routed to valid backend"
        );
        assert_eq!(
            http1_status, 204,
            "http1 request must be routed to valid backend"
        );
        assert_ne!(http2_exit, 0, "http2 request must result in protocol error");
    })
    .await;
}

// === helpers ===

pub fn service(ns: &str) -> k8s::api::core::v1::Service {
    k8s::api::core::v1::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("web".to_string()),
            ..Default::default()
        },
        spec: Some(k8s::api::core::v1::ServiceSpec {
            type_: Some("ClusterIP".to_string()),
            selector: Some(convert_args!(btreemap!(
                "app" => "web"
            ))),
            ports: Some(vec![
                k8s::api::core::v1::ServicePort {
                    name: Some("opaque".to_string()),
                    port: OPAQUE_PORT,
                    target_port: Some(IntOrString::String("http".to_string())),
                    app_protocol: Some("linkerd.io/opaque".to_string()),
                    ..Default::default()
                },
                k8s::api::core::v1::ServicePort {
                    name: Some("http1".to_string()),
                    port: HTTP1_PORT,
                    target_port: Some(IntOrString::String("http".to_string())),
                    app_protocol: Some("http".to_string()),
                    ..Default::default()
                },
                k8s::api::core::v1::ServicePort {
                    name: Some("http2".to_string()),
                    port: HTTP2_PORT,
                    target_port: Some(IntOrString::String("http".to_string())),
                    app_protocol: Some("kubernetes.io/h2c".to_string()),
                    ..Default::default()
                },
            ]),
            ..Default::default()
        }),
        ..Default::default()
    }
}

async fn policy(client: &kube::Client, ns: &str, ip: &str, port: u16) -> OutboundPolicy {
    let mut rx = retry_watch_outbound_policy(client, ns, ip, port).await;
    rx.next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an initial config")
}
