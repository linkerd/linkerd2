ARG RUST_IMAGE=docker.io/library/rust:1.64.0
ARG RUNTIME_IMAGE=gcr.io/distroless/cc

# Builds the controller binary.
FROM $RUST_IMAGE as build
ARG TARGETARCH
ARG BUILD_TYPE="release"
WORKDIR /build
COPY bin/scurl bin/scurl
COPY Cargo.toml Cargo.lock .
COPY policy-controller policy-controller
RUN cargo new policy-test --lib
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.64.0,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo fetch
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.64.0,source=/usr/local/cargo,target=/usr/local/cargo \
    if [ "$BUILD_TYPE" = debug ]; then \
        cargo build --frozen --target=x86_64-unknown-linux-gnu --package=linkerd-policy-controller && \
        mv target/x86_64-unknown-linux-gnu/debug/linkerd-policy-controller /tmp/ ; \
    else \
        cargo build --frozen --target=x86_64-unknown-linux-gnu --release --package=linkerd-policy-controller && \
        mv target/x86_64-unknown-linux-gnu/release/linkerd-policy-controller /tmp/ ; \
    fi

# Creates a minimal runtime image with the controller binary.
FROM $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-policy-controller /bin/
ENTRYPOINT ["/bin/linkerd-policy-controller"]
