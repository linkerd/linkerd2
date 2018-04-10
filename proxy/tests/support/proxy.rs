use support::*;

use std::sync::{Arc, Mutex};

use convert::TryFrom;

pub fn new() -> Proxy {
    Proxy::new()
}

pub struct Proxy {
    controller: Option<controller::Listening>,
    inbound: Option<server::Listening>,
    outbound: Option<server::Listening>,

    metrics_flush_interval: Option<Duration>,
    inbound_disable_ports_protocol_detection: Option<Vec<u16>>,
    outbound_disable_ports_protocol_detection: Option<Vec<u16>>,

    shutdown_signal: Option<Box<Future<Item=(), Error=()> + Send>>,
}

pub struct Listening {
    pub control: SocketAddr,
    pub inbound: SocketAddr,
    pub outbound: SocketAddr,
    pub metrics: SocketAddr,

    pub outbound_server: Option<server::Listening>,
    pub inbound_server: Option<server::Listening>,

    shutdown: Shutdown,
}

impl Proxy {
    pub fn new() -> Self {
        Proxy {
            controller: None,
            inbound: None,
            outbound: None,

            metrics_flush_interval: None,
            inbound_disable_ports_protocol_detection: None,
            outbound_disable_ports_protocol_detection: None,
            shutdown_signal: None,
        }
    }

    pub fn controller(mut self, c: controller::Listening) -> Self {
        self.controller = Some(c);
        self
    }

    pub fn inbound(mut self, s: server::Listening) -> Self {
        self.inbound = Some(s);
        self
    }

    pub fn outbound(mut self, s: server::Listening) -> Self {
        self.outbound = Some(s);
        self
    }

    pub fn metrics_flush_interval(mut self, dur: Duration) -> Self {
        self.metrics_flush_interval = Some(dur);
        self
    }

    pub fn disable_inbound_ports_protocol_detection(mut self, ports: Vec<u16>) -> Self {
        self.inbound_disable_ports_protocol_detection = Some(ports);
        self
    }

    pub fn disable_outbound_ports_protocol_detection(mut self, ports: Vec<u16>) -> Self {
        self.outbound_disable_ports_protocol_detection = Some(ports);
        self
    }

    pub fn shutdown_signal<F>(mut self, sig: F) -> Self
    where
        F: Future + Send + 'static,
    {
        // It doesn't matter what kind of future you give us,
        // we'll just wrap it up in a box and trigger when
        // it triggers. The results are discarded.
        let fut = Box::new(sig.then(|_| Ok(())));
        self.shutdown_signal = Some(fut);
        self
    }

    pub fn run(self) -> Listening {
        self.run_with_test_env(config::TestEnv::new())
    }

    pub fn run_with_test_env(self, mut env: config::TestEnv) -> Listening {
        run(self, env)
    }
}

#[derive(Clone, Debug)]
struct MockOriginalDst(Arc<Mutex<DstInner>>);

#[derive(Debug, Default)]
struct DstInner {
    inbound_orig_addr: Option<SocketAddr>,
    inbound_local_addr: Option<SocketAddr>,
    outbound_orig_addr: Option<SocketAddr>,
    outbound_local_addr: Option<SocketAddr>,
}

impl conduit_proxy::GetOriginalDst for MockOriginalDst {
    fn get_original_dst(&self, sock: &TcpStream) -> Option<SocketAddr> {
        sock.local_addr()
            .ok()
            .and_then(|local| {
                let inner = self.0.lock().unwrap();
                if inner.inbound_local_addr == Some(local) {
                    inner.inbound_orig_addr
                } else if inner.outbound_local_addr == Some(local) {
                    inner.outbound_orig_addr
                } else {
                    None
                }
            })
    }
}


fn run(proxy: Proxy, mut env: config::TestEnv) -> Listening {
    use self::conduit_proxy::config;

    let controller = proxy.controller.expect("proxy controller missing");
    let inbound = proxy.inbound;
    let outbound = proxy.outbound;
    let mut mock_orig_dst = DstInner::default();

    env.put(config::ENV_CONTROL_URL, format!("tcp://{}", controller.addr));
    env.put(config::ENV_PRIVATE_LISTENER, "tcp://127.0.0.1:0".to_owned());
    if let Some(ref inbound) = inbound {
        env.put(config::ENV_PRIVATE_FORWARD, format!("tcp://{}", inbound.addr));
        mock_orig_dst.inbound_orig_addr = Some(inbound.addr);
    }
    if let Some(ref outbound) = outbound {
        mock_orig_dst.outbound_orig_addr = Some(outbound.addr);
    }
    env.put(config::ENV_PUBLIC_LISTENER, "tcp://127.0.0.1:0".to_owned());
    env.put(config::ENV_CONTROL_LISTENER, "tcp://127.0.0.1:0".to_owned());
    env.put(config::ENV_METRICS_LISTENER, "tcp://127.0.0.1:0".to_owned());
    env.put(config::ENV_POD_NAMESPACE, "test".to_owned());

    if let Some(ports) = proxy.inbound_disable_ports_protocol_detection {
        let ports = ports.into_iter()
            .map(|p| p.to_string())
            .collect::<Vec<_>>()
            .join(",");
        env.put(
            config::ENV_INBOUND_PORTS_DISABLE_PROTOCOL_DETECTION,
            ports
        );
    }

    if let Some(ports) = proxy.outbound_disable_ports_protocol_detection {
        let ports = ports.into_iter()
            .map(|p| p.to_string())
            .collect::<Vec<_>>()
            .join(",");
        env.put(
            config::ENV_OUTBOUND_PORTS_DISABLE_PROTOCOL_DETECTION,
            ports
        );
    }

    let mut config = config::Config::try_from(&env).unwrap();

    // TODO: We currently can't use `config::ENV_METRICS_FLUSH_INTERVAL_SECS`
    // because we need to be able to set the flush interval to a fraction of a
    // second. We should change config::ENV_METRICS_FLUSH_INTERVAL_SECS so that
    // it can support this.
    if let Some(dur) = proxy.metrics_flush_interval {
        config.metrics_flush_interval = dur;
    }

    let (running_tx, running_rx) = oneshot::channel();
    let (tx, mut rx) = shutdown_signal();

    if let Some(fut) = proxy.shutdown_signal {
        rx = Box::new(rx.select(fut).then(|_| Ok(())));
    }

    ::std::thread::Builder::new()
        .name("support proxy".into())
        .spawn(move || {
            let _c = controller;

            let mock_orig_dst = MockOriginalDst(Arc::new(Mutex::new(mock_orig_dst)));

            let main = conduit_proxy::Main::new(config, mock_orig_dst.clone());

            let control_addr = main.control_addr();
            let inbound_addr = main.inbound_addr();
            let outbound_addr = main.outbound_addr();
            let metrics_addr = main.metrics_addr();

            {
                let mut inner = mock_orig_dst.0.lock().unwrap();
                inner.inbound_local_addr = Some(inbound_addr);
                inner.outbound_local_addr = Some(outbound_addr);
            }

            // slip the running tx into the shutdown future, since the first time
            // the shutdown future is polled, that means all of the proxy is now
            // running.
            let addrs = (
                control_addr,
                inbound_addr,
                outbound_addr,
                metrics_addr,
            );
            let mut running = Some((running_tx, addrs));
            let on_shutdown = future::poll_fn(move || {
                if let Some((tx, addrs)) = running.take() {
                    let _ = tx.send(addrs);
                }

                rx.poll()
            });

            main.run_until(on_shutdown);
        })
        .unwrap();

    let (control_addr, inbound_addr, outbound_addr, metrics_addr) =
        running_rx.wait().unwrap();

    Listening {
        control: control_addr,
        inbound: inbound_addr,
        outbound: outbound_addr,
        metrics: metrics_addr,

        outbound_server: outbound,
        inbound_server: inbound,

        shutdown: tx,
    }
}
