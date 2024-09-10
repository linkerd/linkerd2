FROM --platform=$BUILDPLATFORM ghcr.io/linkerd/dev:v43-rust-musl as controller
ARG BUILD_TYPE="release"
WORKDIR /build
RUN mkdir -p target/bin
COPY Cargo.toml Cargo.lock .
COPY policy-controller policy-controller
RUN cargo new policy-test --lib
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    cargo fetch
ARG TARGETARCH
# Enable tokio runtime metrics
ENV RUSTFLAGS="--cfg tokio_unstable"
RUN --mount=type=cache,target=target \
    --mount=type=cache,target=/usr/local/cargo/registry \
    target=$(case "$TARGETARCH" in \
        amd64) echo x86_64-unknown-linux-musl ;; \
        arm64) echo aarch64-unknown-linux-musl ;; \
        arm) echo armv7-unknown-linux-musleabihf ;; \
        *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac) && \
    just-cargo profile=$BUILD_TYPE target=$target build --package=linkerd-policy-controller && \
    mv "target/$target/$BUILD_TYPE/linkerd-policy-controller" /tmp/

FROM scratch as runtime
LABEL org.opencontainers.image.source=https://github.com/linkerd/linkerd2
COPY --from=controller /tmp/linkerd-policy-controller /bin/
ENTRYPOINT ["/bin/linkerd-policy-controller"]
