# Proxy build and runtime
#
# Builds a slim runtime image with the conduit-proxy binary.

ARG RUST_IMAGE=rust:1.23.0

ARG RUNTIME_IMAGE=gcr.io/runconduit/base:2017-10-30.01

## Fetch external dependencies.
#
# Mocks out all local code and fetch external dependencies to ensure that external sources
# are cached.
FROM $RUST_IMAGE as fetch
WORKDIR /usr/src/conduit
RUN for d in proxy proxy/convert proxy/futures-mpsc-lossy proxy/grpc proxy/tower-router ; \
    do mkdir -p "${d}/src" && touch "${d}/src/lib.rs" ; \
    done
COPY Cargo.toml Cargo.lock ./
COPY proxy/Cargo.toml                     proxy/Cargo.toml
COPY proxy/convert/Cargo.toml             proxy/convert/Cargo.toml
COPY proxy/futures-mpsc-lossy/Cargo.toml  proxy/futures-mpsc-lossy/Cargo.toml
COPY proxy/grpc/Cargo.toml                proxy/grpc/Cargo.toml
COPY proxy/tower-router/Cargo.toml        proxy/tower-router/Cargo.toml
RUN cargo fetch --locked

## Build libraries.
#
# Mocks proxy sources and gRPC bindings to ensure that dependencies are cached.
FROM $RUST_IMAGE as libs
WORKDIR /usr/src/conduit
COPY --from=fetch $CARGO_HOME $CARGO_HOME
RUN for d in proxy proxy/grpc ; \
    do mkdir -p "${d}/src" && touch "${d}/src/lib.rs" ; \
    done
COPY Cargo.toml Cargo.lock ./
COPY proxy/Cargo.toml          ./proxy/Cargo.toml
COPY proxy/convert             ./proxy/convert
COPY proxy/futures-mpsc-lossy  ./proxy/futures-mpsc-lossy
COPY proxy/grpc/Cargo.toml     ./proxy/grpc/Cargo.toml
COPY proxy/tower-router        ./proxy/tower-router
ARG DEBUG
RUN if [ -n "$DEBUG" ]; \
    then cargo build --frozen ; \
    else cargo build --frozen --release ; \
    fi
#RUN if [ -n "$SKIP_TESTS" ]; \
#    then echo "tests skipped" ; \
#    elif [ -n "$DEBUG" ]; \
#    then cargo test --frozen ; \
#    else cargo test --frozen --release ; \
#    fi

## Build gRPC bindings.
#
# Mocks proxy sources to ensure that dependencies, including build-time dependencies
# needed to generate the bindings, are cached.
FROM $RUST_IMAGE as grpc
WORKDIR /usr/src/conduit
COPY --from=fetch $CARGO_HOME $CARGO_HOME
RUN for d in proxy ; \
    do mkdir -p "${d}/src" && touch "${d}/src/lib.rs" ; \
    done
COPY Cargo.toml Cargo.lock ./
COPY proto                     ./proto
COPY proxy/Cargo.toml          ./proxy/Cargo.toml
COPY proxy/convert             ./proxy/convert
COPY proxy/futures-mpsc-lossy  ./proxy/futures-mpsc-lossy
COPY proxy/grpc                ./proxy/grpc
COPY proxy/tower-router        ./proxy/tower-router
COPY --from=libs /usr/src/conduit/target ./target
ARG DEBUG
RUN if [ -n "$DEBUG" ]; \
    then cargo build -p conduit-grpc --features=arbitrary --frozen ; \
    else cargo build -p conduit-grpc --features=arbitrary --frozen --release ; \
    fi
#RUN if [ -n "$SKIP_TESTS" ]; \
#    then echo "tests skipped" ; \
#    elif [ -n "$DEBUG" ]; \
#    then cargo test -p conduit-grpc --features=arbitrary --frozen ; \
#    else cargo test -p conduit-grpc --features=arbitrary --frozen --release ; \
#    fi

## Build the proxy binary using pre-built dependencies.
FROM $RUST_IMAGE as bin
WORKDIR /usr/src/conduit
COPY --from=fetch $CARGO_HOME $CARGO_HOME
COPY Cargo.toml Cargo.lock ./
COPY proto ./proto
COPY proxy ./proxy
COPY --from=grpc /usr/src/conduit/target ./target
ARG DEBUG
RUN if [ -n "$DEBUG" ]; \
    then cargo build -p conduit-proxy --bin conduit-proxy --frozen ; \
    else cargo build -p conduit-proxy --bin conduit-proxy --frozen --release ; \
    fi
ARG SKIP_TESTS
RUN if [ -n "$SKIP_TESTS" ]; \
    then echo "tests skipped" ; \
    elif [ -n "$DEBUG" ]; \
    then cargo test -p conduit-proxy --frozen ; \
    else cargo test -p conduit-proxy --frozen --release ; \
    fi
RUN if [ -n "$DEBUG" ]; \
    then mv target/debug/conduit-proxy   target/conduit-proxy ; \
    else mv target/release/conduit-proxy target/conduit-proxy ; \
    fi

## Install the proxy binary into the base runtime image.
FROM $RUNTIME_IMAGE as runtime
COPY --from=bin /usr/src/conduit/target/conduit-proxy /usr/local/bin/conduit-proxy
ENV CONDUIT_PROXY_LOG=warn,conduit_proxy=info
ENTRYPOINT ["/usr/local/bin/conduit-proxy"]
