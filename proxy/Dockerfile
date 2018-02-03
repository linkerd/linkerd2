# Proxy build and runtime
#
# Builds a slim runtime image with the conduit-proxy binary.

## Mock out all local library code and fetch external dependencies.
FROM rust:1.23.0 as fetch
WORKDIR /usr/src/conduit
RUN mkdir -p \
        proxy/src \
        proxy/convert/src \
        proxy/futures-mpsc-lossy/src \
        proxy/grpc/src \
        proxy/tower-router/src \
    && touch \
        proxy/src/lib.rs \
        proxy/convert/src/lib.rs \
        proxy/futures-mpsc-lossy/src/lib.rs \
        proxy/grpc/src/lib.rs \
        proxy/tower-router/src/lib.rs
COPY Cargo.toml Cargo.lock                ./
COPY proxy/Cargo.toml                     proxy/Cargo.toml
COPY proxy/convert/Cargo.toml             proxy/convert/Cargo.toml
COPY proxy/futures-mpsc-lossy/Cargo.toml  proxy/futures-mpsc-lossy/Cargo.toml
COPY proxy/grpc/Cargo.toml                proxy/grpc/Cargo.toml
COPY proxy/tower-router/Cargo.toml        proxy/tower-router/Cargo.toml
RUN cargo fetch --locked

## Mock out the proxy source and build all libraries.
#
# Because the proto build has build-time dependencies, this step allows these dependencies
# to be cached across proxy source rebuilds.
FROM rust:1.23.0 as libs
WORKDIR /usr/src/conduit
COPY --from=fetch $CARGO_HOME $CARGO_HOME
RUN mkdir -p proxy/src \
    && touch proxy/src/lib.rs
COPY Cargo.toml Cargo.lock     ./
COPY proto                     ./proto
COPY proxy/Cargo.toml          ./proxy/Cargo.toml
COPY proxy/convert             ./proxy/convert
COPY proxy/futures-mpsc-lossy  ./proxy/futures-mpsc-lossy
COPY proxy/tower-router        ./proxy/tower-router
COPY proxy/grpc                ./proxy/grpc
ARG RELEASE
RUN if [ -z "$RELEASE" ]; \
    then cargo build --frozen ; \
    else cargo build --frozen --release ; \
    fi

## Build the proxy binary using pre-built dependencies.
FROM rust:1.23.0 as bin
WORKDIR /usr/src/conduit
COPY --from=fetch $CARGO_HOME $CARGO_HOME
COPY Cargo.toml Cargo.lock  ./
COPY proto                  ./proto
COPY proxy                  ./proxy
ARG RELEASE
COPY --from=libs /usr/src/conduit/target ./target
RUN if [ -z "$RELEASE" ]; then \
        cargo build -p conduit-proxy --bin conduit-proxy --frozen && \
        mv target/debug/conduit-proxy target/conduit-proxy ; \
    else \
        cargo build -p conduit-proxy --bin conduit-proxy --frozen --release && \
        mv target/release/conduit-proxy target/conduit-proxy ; \
    fi

## Install the proxy binary into the base runtime image.
FROM gcr.io/runconduit/base:2017-10-30.01
COPY --from=bin /usr/src/conduit/target/conduit-proxy /usr/local/bin/conduit-proxy
ENV CONDUIT_PROXY_LOG=warn,conduit_proxy=info
ENTRYPOINT ["/usr/local/bin/conduit-proxy"]
