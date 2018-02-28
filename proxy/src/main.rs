#![deny(warnings)]
extern crate conduit_proxy;
extern crate tokio_timer;
use std::process;

// Look in lib.rs.
fn main() {
    // Load configuration.
    let config = match conduit_proxy::app::init() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("configuration error: {:#?}", e);
            process::exit(64)
        }
    };
    let timer = tokio_timer::Timer::default();
    conduit_proxy::Main::new(config, conduit_proxy::SoOriginalDst, timer).run();
}
