ARG RUST_VERSION=1.60.0
ARG RUST_IMAGE=docker.io/library/rust:${RUST_VERSION}
ARG RUNTIME_IMAGE=gcr.io/distroless/cc

# Builds the operator binary.
FROM $RUST_IMAGE as build
RUN apt-get update && \
    apt-get install -y --no-install-recommends g++-aarch64-linux-gnu libc6-dev-arm64-cross && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/ && \
    rustup target add aarch64-unknown-linux-gnu
ENV CARGO_TARGET_AARCH64_UNKNOWN_LINUX_GNU_LINKER=aarch64-linux-gnu-gcc
WORKDIR /build
COPY Cargo.toml Cargo.lock .
COPY cni-plugin/linkerd-cni-validator /build/
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo fetch --locked
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo build --locked --target=aarch64-unknown-linux-gnu --release --package=linkerd-cni-validator && \
    mv target/aarch64-unknown-linux-gnu/release/linkerd-cni-validator /tmp/

FROM $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-cni-validator /bin/
ENTRYPOINT ["/bin/linkerd-cni-validator"]
