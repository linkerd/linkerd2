#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    linkerd_policy_controller_runtime::Args::parse_and_run().await
}
