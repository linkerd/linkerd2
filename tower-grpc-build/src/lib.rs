extern crate codegen;
extern crate prost_build;

mod client;
mod server;

use std::io;
use std::cell::RefCell;
use std::fmt::Write;
use std::path::Path;
use std::rc::Rc;

/// Code generation configuration
pub struct Config {
    prost: prost_build::Config,
    inner: Rc<RefCell<Inner>>,
}

struct Inner {
    build_client: bool,
    build_server: bool,
}

struct ServiceGenerator {
    client: client::ServiceGenerator,
    server: server::ServiceGenerator,
    inner: Rc<RefCell<Inner>>,
}

impl Config {
    /// Returns a new `Config` with default values.
    pub fn new() -> Self {
        let mut prost = prost_build::Config::new();

        let inner = Rc::new(RefCell::new(Inner {
            // Enable client code gen by default
            build_client: true,

            // Disable server code gen by default
            build_server: false,
        }));

        // Set the service generator
        prost.service_generator(Box::new(ServiceGenerator {
            client: client::ServiceGenerator,
            server: server::ServiceGenerator,
            inner: inner.clone(),
        }));

        Config {
            prost,
            inner,
        }
    }

    /// Enable gRPC client code generation
    pub fn enable_client(&mut self, enable: bool) -> &mut Self {
        self.inner.borrow_mut().build_client = enable;
        self
    }

    /// Enable gRPC server code generation
    pub fn enable_server(&mut self, enable: bool) -> &mut Self {
        self.inner.borrow_mut().build_server = enable;
        self
    }

    /// Generate code
    pub fn build<P>(&self, protos: &[P], includes: &[P]) -> io::Result<()>
    where P: AsRef<Path>,
    {
        self.prost.compile_protos(protos, includes)
    }
}

impl prost_build::ServiceGenerator for ServiceGenerator {
    fn generate(&self, service: prost_build::Service, buf: &mut String) {
        let inner = self.inner.borrow();

        if inner.build_client {
            // Add an extra new line to separate messages
            write!(buf, "\n").unwrap();

            self.client.generate(&service, buf).unwrap();
        }

        if inner.build_server {
            write!(buf, "\n\n").unwrap();
            self.server.generate(&service, buf).unwrap();
        }
    }
}
