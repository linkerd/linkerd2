#![deny(warnings)]

extern crate conduit_proxy;

#[macro_use] extern crate log;

use std::process;

mod signal;

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
    let main = conduit_proxy::Main::new(config, conduit_proxy::SoOriginalDst);
    let shutdown_signal = signal::shutdown(&main.handle());
    main.run_until(shutdown_signal);
}
