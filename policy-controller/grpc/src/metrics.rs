use std::{pin::Pin, time::Instant};

use futures::Future;
use futures::FutureExt;
use http::{Request, Response};
use hyper::Body;
use prometheus_client::metrics::{counter::Counter, family::Family, histogram::Histogram};
use std::task::{Context, Poll};
use tower::{Layer, Service};

struct ResponseLabels {
    path: String,
    status: u16,
}

pub struct MetricsLayer {
    responses: Family<ResponseLabels, Counter>,
    response_errors: Family<ResponseLabels, Counter>,
    response_latency: Family<ResponseLabels, Histogram>,
}

struct MetricsService<S> {
    inner: S,
    responses: Family<ResponseLabels, Counter>,
    response_errors: Family<ResponseLabels, Counter>,
    response_latency: Family<ResponseLabels, Histogram>,
}

struct MetricsFuture<F> {
    response: F,
    path: String,
    start: Instant,
    responses: Family<ResponseLabels, Counter>,
    response_errors: Family<ResponseLabels, Counter>,
    response_latency: Family<ResponseLabels, Histogram>,
}

impl<S> Layer<S> for MetricsLayer {
    type Service = MetricsService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        MetricsService {
            inner,
            responses: self.responses.clone(),
            response_errors: self.response_errors.clone(),
            response_latency: self.response_latency.clone(),
        }
    }
}

impl<S> Service<Request<Body>> for MetricsService<S>
where
    S: Service<Request<Body>>,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    async fn call(&mut self, request: Request<Body>) -> Self::Response {
        let path = request.uri().path().to_string();
        let start = Instant::now();

        let response = self.inner.call(request).await;

        let latency = start.elapsed();
        self.response_latency
            .get_or_create(ResponseLabels {
                path: path.clone(),
                status,
            })
            .observe(latency.as_secs_f64());

        match &response {
            Ok(response) => {
                let status = response.status().as_u16();

                self.responses
                    .get_or_create(ResponseLabels {
                        path: path.clone(),
                        status,
                    })
                    .inc();
            }
            Err(_) => {
                self.response_errors
                    .get_or_create(ResponseLabels {
                        path: path.clone(),
                        status: 0,
                    })
                    .inc();
            }
        }

        response
    }
}

impl<F, E> Future for MetricsFuture<F>
where
    F: Future<Output = Result<Response<Body>, E>>,
{
    type Output = F::Output;

    fn poll(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output> {
        match self.response.poll_unpin(cx) {
            Poll::Ready(Ok(response)) => {
                let status = response.status().as_u16();
                let latency = self.start.elapsed();

                self.responses
                    .get_or_create(ResponseLabels {
                        path: self.path.clone(),
                        status,
                    })
                    .inc();

                if status >= 400 {
                    self.response_errors
                        .get_or_create(ResponseLabels {
                            path: self.path.clone(),
                            status,
                        })
                        .inc();
                }

                self.response_latency
                    .get_or_create(ResponseLabels {
                        path: self.path.clone(),
                        status,
                    })
                    .observe(latency.as_secs_f64());

                Poll::Ready(Ok(response))
            }
            Poll::Ready(Err(error)) => Poll::Ready(Err(error)),
            Poll::Pending => Poll::Pending,
        }
    }
}
