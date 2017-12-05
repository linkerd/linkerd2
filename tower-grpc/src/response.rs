use http;

#[derive(Debug)]
pub struct Response<T> {
    http: http::Response<T>,
}

impl<T> Response<T> {
    pub fn new(message: T) -> Self {
        let mut res = http::Response::new(message);
        *res.version_mut() = http::Version::HTTP_2;

        Response {
            http: res,
        }
    }

    pub(crate) fn from_http(res: http::Response<T>) -> Self {
        Response {
            http: res,
        }
    }

    pub fn into_http(self) -> http::Response<T> {
        self.http
    }

    pub fn map<F, U>(self, f: F) -> Response<U>
    where F: FnOnce(T) -> U,
    {
        let (head, body) = self.http.into_parts();
        let body = f(body);
        let http = http::Response::from_parts(head, body);
        Response::from_http(http)
    }

    // pub fn metadata()
    // pub fn metadata_bin()
}
