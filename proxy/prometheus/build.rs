extern crate prost_build;

fn main() {
    prost_build::compile_protos(&["proto/metrics.proto"],
                                &["proto/"])
        .expect("failed to compile Prometheus protocol buffers");
}
