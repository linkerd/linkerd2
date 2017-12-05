use futures::future::{self, FutureResult};
use tower::{Service, NewService};

pub struct NewServiceFn<T> {
    f: T,
}

impl<T, N> NewServiceFn<T>
where T: Fn() -> N,
      N: Service,
{
    pub fn new(f: T) -> Self {
        NewServiceFn { f }
    }
}

impl<T, N> NewService for NewServiceFn<T>
where T: Fn() -> N,
      N: Service,
{
    type Request = N::Request;
    type Response = N::Response;
    type Error = N::Error;
    type Service = N;
    type InitError = ();
    type Future = FutureResult<Self::Service, Self::InitError>;

    fn new_service(&self) -> Self::Future {
        future::ok((self.f)())
    }
}
