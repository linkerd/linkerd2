FROM --platform=$BUILDPLATFORM ghcr.io/linkerd/dev:v47-rust-musl AS controller
ARG BUILD_TYPE="release"
WORKDIR /build
RUN mkdir -p target/bin
COPY Cargo.toml Cargo.lock .
COPY policy-controller policy-controller
RUN cargo new policy-test --lib
ENV CARGO="cargo auditable"
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    just-cargo fetch
ARG TARGETARCH
# Install cross compiler toolchains
RUN case "$TARGETARCH" in \
        amd64) true ;; \
        arm64) apt-get -y update && apt-get install --no-install-recommends -y gcc-aarch64-linux-gnu binutils-aarch64-linux-gnu ;; \
    esac && rm -rf /var/lib/apt/lists/*
# Enable tokio runtime metrics
ENV RUSTFLAGS="--cfg tokio_unstable"
ENV AWS_LC_SYS_CFLAGS_aarch64_unknown_linux_gnu='-fuse-ld=/usr/aarch64-linux-gnu/bin/ld'
RUN --mount=type=cache,target=target \
    --mount=type=cache,target=/usr/local/cargo/registry \
    target=$(case "$TARGETARCH" in \
        amd64) echo x86_64-unknown-linux-musl ;; \
        arm64) echo aarch64-unknown-linux-musl ;; \
        *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac) && \
    just-cargo profile=$BUILD_TYPE target=$target build --package=linkerd-policy-controller && \
    mv "target/$target/$BUILD_TYPE/linkerd-policy-controller" /tmp/

FROM scratch AS runtime
LABEL org.opencontainers.image.source=https://github.com/linkerd/linkerd2
COPY --from=controller /tmp/linkerd-policy-controller /bin/
ENTRYPOINT ["/bin/linkerd-policy-controller"]
