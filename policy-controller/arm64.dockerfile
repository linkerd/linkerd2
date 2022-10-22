ARG RUST_IMAGE=docker.io/library/rust:1.64.0
ARG RUNTIME_IMAGE=gcr.io/distroless/cc

FROM $RUST_IMAGE as build
RUN apt-get update && \
    apt-get install -y --no-install-recommends g++-aarch64-linux-gnu libc6-dev-arm64-cross && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/ && \
    rustup target add aarch64-unknown-linux-gnu
ENV CARGO_TARGET_AARCH64_UNKNOWN_LINUX_GNU_LINKER=aarch64-linux-gnu-gcc
WORKDIR /build
COPY bin/scurl bin/scurl
COPY Cargo.toml Cargo.lock .
COPY policy-controller policy-controller
RUN cargo new policy-test --lib
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.64.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo fetch
# XXX(ver) we can't easily cross-compile against openssl, so use rustls on arm.
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.64.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo build --frozen --release --target=aarch64-unknown-linux-gnu \
        --package=linkerd-policy-controller --no-default-features --features="rustls-tls" && \
    mv target/aarch64-unknown-linux-gnu/release/linkerd-policy-controller /tmp/

FROM --platform=linux/arm64 $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-policy-controller /bin/
ENTRYPOINT ["/bin/linkerd-policy-controller"]
