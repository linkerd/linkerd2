use support::*;

use support::conduit_proxy::convert::TryFrom;

pub fn new() -> Proxy {
    Proxy::new()
}

#[derive(Debug)]
pub struct Proxy {
    controller: Option<controller::Listening>,
    inbound: Option<server::Listening>,
    outbound: Option<server::Listening>,

    metrics_flush_interval: Option<Duration>,
}

#[derive(Debug)]
pub struct Listening {
    pub control: SocketAddr,
    pub inbound: SocketAddr,
    pub outbound: SocketAddr,

    shutdown: Shutdown,
}

impl Proxy {
    pub fn new() -> Self {
        Proxy {
            controller: None,
            inbound: None,
            outbound: None,

            metrics_flush_interval: None,
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

    pub fn run(self) -> Listening {
        run(self)
    }
}

fn run(proxy: Proxy) -> Listening {
    use self::conduit_proxy::config;

    let controller = proxy.controller.expect("proxy controller missing");
    let inbound = proxy.inbound;
    let outbound = proxy.outbound;

    let mut env = config::TestEnv::new();
    env.put(config::ENV_CONTROL_URL, format!("tcp://{}", controller.addr));
    env.put(config::ENV_PRIVATE_LISTENER, "tcp://127.0.0.1:0".to_owned());
    if let Some(ref inbound) = inbound {
        env.put(config::ENV_PRIVATE_FORWARD, format!("tcp://{}", inbound.addr));
    }
    env.put(config::ENV_PUBLIC_LISTENER, "tcp://127.0.0.1:0".to_owned());
    env.put(config::ENV_CONTROL_LISTENER, "tcp://127.0.0.1:0".to_owned());

    let mut config = config::Config::try_from(&env).unwrap();

    // TODO: We currently can't use `config::ENV_METRICS_FLUSH_INTERVAL_SECS`
    // because we need to be able to set the flush interval to a fraction of a
    // second. We should change config::ENV_METRICS_FLUSH_INTERVAL_SECS so that
    // it can support this.
    if let Some(dur) = proxy.metrics_flush_interval {
        config.metrics_flush_interval = dur;
    }

    let main = conduit_proxy::Main::new(config);

    let control_addr = main.control_addr();
    let inbound_addr = main.inbound_addr();
    let outbound_addr = main.outbound_addr();

    let (running_tx, running_rx) = shutdown_signal();
    let (tx, rx) = shutdown_signal();

    ::std::thread::Builder::new()
        .name("support proxy".into())
        .spawn(move || {
            let _c = controller;
            let _i = inbound;
            let _o = outbound;

            let _ = running_tx.send(());
            main.run_until(rx);
        })
        .unwrap();

    running_rx.wait().unwrap();
    ::std::thread::sleep(::std::time::Duration::from_millis(100));

    Listening {
        control: control_addr,
        inbound: inbound_addr,
        outbound: outbound_addr,
        shutdown: tx,
    }
}
