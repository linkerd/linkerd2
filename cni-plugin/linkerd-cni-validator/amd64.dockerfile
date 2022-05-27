ARG RUST_VERSION=1.60.0
ARG RUST_IMAGE=docker.io/library/rust:${RUST_VERSION}
ARG RUNTIME_IMAGE=gcr.io/distroless/cc

# Builds the operator binary.
FROM $RUST_IMAGE as build
WORKDIR /build
RUN cargo new policy-controller --lib && \
    cargo new policy-controller/core --lib && \
    cargo new policy-controller/grpc --lib && \
    cargo new policy-controller/k8s/api --lib && \
    cargo new policy-controller/k8s/index --lib && \
    cargo new policy-test --lib
COPY Cargo.toml Cargo.lock .
COPY cni-plugin/linkerd-cni-validator cni-plugin/linkerd-cni-validator
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo fetch
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.60.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo build --locked --target=x86_64-unknown-linux-gnu --release --package=linkerd-cni-validator && \
    mv target/x86_64-unknown-linux-gnu/release/linkerd-cni-validator /tmp/

FROM $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-cni-validator /bin/
ENTRYPOINT ["/bin/linkerd-cni-validator"]
