ARG RUST_VERSION=1.60.0
ARG RUST_IMAGE=docker.io/library/rust:${RUST_VERSION}
ARG RUNTIME_IMAGE=gcr.io/distroless/cc

# Builds the operator binary.
FROM $RUST_IMAGE as build
RUN apt-get update && \
    apt-get install -y --no-install-recommends g++-arm-linux-gnueabihf libc6-dev-armhf-cross && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/ && \
    rustup target add armv7-unknown-linux-gnueabihf
ENV CARGO_TARGET_ARMV7_UNKNOWN_LINUX_GNUEABIHF_LINKER=arm-linux-gnueabihf-gcc
WORKDIR /build
COPY Cargo.toml Cargo.lock .
COPY cni-plugin/linkerd-cni-validator /build/
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo fetch --locked
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo build --locked --target=armv7-unknown-linux-gnueabihf --release --package=linkerd-cni-validator && \
    mv target/armv7-unknown-linux-gnueabihf/release/linkerd-cni-validator /tmp/

FROM $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-cni-validator /bin/
ENTRYPOINT ["/bin/linkerd-cni-validator"]
