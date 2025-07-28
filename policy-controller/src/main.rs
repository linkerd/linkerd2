#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    #[cfg(feature = "rustls-tls")]
    {
        if rustls::crypto::aws_lc_rs::default_provider()
            .install_default()
            .is_err()
        {
            anyhow::bail!("No other crypto provider should be installed yet");
        }
    }

    linkerd_policy_controller_runtime::Args::parse_and_run().await
}
