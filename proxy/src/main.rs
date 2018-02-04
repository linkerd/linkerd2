#![deny(warnings)]
extern crate conduit_proxy;

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
    conduit_proxy::Main::new(config, conduit_proxy::SoOriginalDst).run();
}
