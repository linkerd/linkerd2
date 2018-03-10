extern crate prost;
#[macro_use]
extern crate prost_derive;

pub mod pb {
    include!(concat!(env!("OUT_DIR"), "/io.prometheus.client.rs"));
}
