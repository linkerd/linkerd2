use std::time::Instant;

pub trait Now: Clone {
    fn now(&self) -> Instant;

    fn into_tokio(self) -> Tokio<Self> {
        Tokio(self)
    }
}

pub struct Tokio<T>(T);

#[derive(Clone, Debug)]
pub struct SystemNow;

// ===== impl SystemNow =====

impl Now for SystemNow {
    fn now(&self) -> Instant {
        Instant::now()
    }
}

// ===== impl Tokio =====

impl<T: Now> ::tokio_timer::timer::Now for Tokio<T> {
    fn now(&mut self) -> Instant {
        self.0.now()
    }
}

/// A mocked instance of `Now` to drive tests.
#[cfg(test)]
mod test_util {
    use super::Now;
    use std::sync::{Arc, Mutex};
    use std::time::{Duration, Instant};

    #[derive(Clone)]
    pub struct Clock(Arc<Mutex<Instant>>);

    // ===== impl Clock =====

    impl Default for Clock {
        fn default() -> Clock {
            Clock(Arc::new(Mutex::new(Instant::now())))
        }
    }

    impl Clock {
        pub fn advance(&self, d: Duration) {
            let mut instant = *self.0.lock().expect("lock test time source");
            instant += d;
        }
    }

    impl Now for Clock {
        fn now(&self) -> Instant {
            let instant = *self.0.lock().expect("lock test time source");
            instant.clone()
        }
    }
}
