extern crate tower_grpc_build;

fn main() {
    build_control();
}

fn build_control() {
    let client_files = &[
        "../proto/common/common.proto",
        "../proto/proxy/destination/destination.proto",
        "../proto/proxy/telemetry/telemetry.proto",
    ];
    let server_files = &[
        "../proto/proxy/tap/tap.proto",
    ];
    let dirs = &[
        "../proto",
    ];

    tower_grpc_build::Config::new()
        .enable_client(true)
        .enable_server(false)
        .build(client_files, dirs)
        .unwrap();

    tower_grpc_build::Config::new()
        .enable_client(false)
        .enable_server(true)
        .build(server_files, dirs)
        .unwrap();
}
