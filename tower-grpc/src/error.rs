use h2;

#[derive(Debug)]
pub enum Error<T = ()> {
    Grpc(::Status),
    Inner(T),
}

impl<T> From<T> for Error<T> {
    fn from(inner: T) -> Self {
        Error::Inner(inner)
    }
}

impl From<Error<()>> for h2::Error {
    fn from(_err: Error<()>) -> Self {
        // TODO: implement
        h2::Reason::INTERNAL_ERROR.into()
    }
}

impl From<h2::Error> for Error<()> {
    fn from(_: h2::Error) -> Self {
        // TODO: implement
        Error::Inner(())
    }
}
