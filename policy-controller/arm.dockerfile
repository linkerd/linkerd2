ARG RUST_IMAGE=docker.io/library/rust:1.56.0
ARG RUNTIME_IMAGE=gcr.io/distroless/cc

FROM $RUST_IMAGE as build
RUN apt-get update && \
    apt-get install -y --no-install-recommends g++-arm-linux-gnueabihf libc6-dev-armhf-cross && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/ && \
    rustup target add armv7-unknown-linux-gnueabihf
ENV CARGO_TARGET_ARMV7_UNKNOWN_LINUX_GNUEABIHF_LINKER=arm-linux-gnueabihf-gcc
WORKDIR /build
COPY Cargo.toml Cargo.lock policy-controller/ /build/
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.56.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo build --locked --release --target=armv7-unknown-linux-gnueabihf --package=linkerd-policy-controller && \
    mv target/armv7-unknown-linux-gnueabihf/release/linkerd-policy-controller /tmp/

FROM --platform=linux/arm $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-policy-controller /bin/
ENTRYPOINT ["/bin/linkerd-policy-controller"]
