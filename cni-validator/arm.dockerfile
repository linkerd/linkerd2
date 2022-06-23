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
RUN cargo new policy-controller --lib && \
    cargo new policy-controller/core --lib && \
    cargo new policy-controller/grpc --lib && \
    cargo new policy-controller/k8s/api --lib && \
    cargo new policy-controller/k8s/index --lib && \
    cargo new policy-test --lib
COPY Cargo.toml Cargo.lock .
COPY cni-validator /build/
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo fetch
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo build --locked --target=armv7-unknown-linux-gnueabihf --release --package=cni-validator && \
    mv target/armv7-unknown-linux-gnueabihf/release/cni-validator /tmp/

FROM $RUNTIME_IMAGE
COPY --from=build /tmp/cni-validator /bin/
ENTRYPOINT ["/bin/cni-validator"]
