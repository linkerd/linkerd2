extern crate prost_build;

fn main() {
    let proto_path = "../../proto/proxy/prometheus/metrics.proto";

    prost_build::compile_protos(&[proto_path], &["../../proto/"])
        .expect("failed to compile Prometheus protocol buffers");

    println!("cargo:rerun-if-changed={}", proto_path);
}
