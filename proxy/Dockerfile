# Proxy build and runtime
#
# Builds a slim runtime image with the conduit-proxy binary.

FROM rust:1.23.0 as fetch
WORKDIR /usr/src/conduit
RUN mkdir -p futures-mpsc-lossy/src tower-router/src proxy/src
RUN touch futures-mpsc-lossy/src/lib.rs tower-router/src/lib.rs proxy/src/lib.rs
COPY Cargo.toml Cargo.lock          ./
COPY futures-mpsc-lossy/Cargo.toml  futures-mpsc-lossy/Cargo.toml
COPY tower-router/Cargo.toml        tower-router/Cargo.toml
COPY proxy/Cargo.toml               proxy/Cargo.toml
RUN cargo fetch --locked

FROM rust:1.23.0 as deps
WORKDIR /usr/src/conduit
COPY --from=fetch $CARGO_HOME $CARGO_HOME
COPY Cargo.toml Cargo.lock  ./
COPY futures-mpsc-lossy     ./futures-mpsc-lossy
COPY tower-router           ./tower-router
COPY proxy/Cargo.toml       proxy/Cargo.toml
RUN mkdir proxy/src && \
    touch proxy/src/lib.rs
ARG RELEASE
RUN if [ -z "$RELEASE" ]; \
    then cargo build --frozen ; \
    else cargo build --frozen --release ;  fi

FROM rust:1.23.0 as bin
WORKDIR /usr/src/conduit
COPY --from=deps $CARGO_HOME $CARGO_HOME
COPY Cargo.toml Cargo.lock  ./
COPY futures-mpsc-lossy     ./futures-mpsc-lossy
COPY tower-router           ./tower-router
COPY proto                  ./proto
COPY proxy                  ./proxy
ARG RELEASE
COPY --from=deps /usr/src/conduit/target ./target
RUN if [ -z "$RELEASE" ]; then \
        cargo build --frozen -p conduit-proxy --bin conduit-proxy && \
        mv target/debug/conduit-proxy target/conduit-proxy ; \
    else \
        cargo build --frozen -p conduit-proxy --bin conduit-proxy --release && \
        mv target/release/conduit-proxy target/conduit-proxy ; \
    fi

## Install the proxy binary into the base runtime image.
FROM gcr.io/runconduit/base:2017-10-30.01
COPY --from=bin /usr/src/conduit/target/conduit-proxy /usr/local/bin/conduit-proxy
ENV CONDUIT_PROXY_LOG=warn,conduit_proxy=info
ENTRYPOINT ["/usr/local/bin/conduit-proxy"]
