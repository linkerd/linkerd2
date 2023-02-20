FROM --platform=$BUILDPLATFORM ghcr.io/linkerd/dev:v39-rust as builder
ARG BUILD_TYPE="release"
WORKDIR /build
RUN mkdir -p target/bin
COPY Cargo.toml Cargo.lock .
COPY ns-labeler ns-labeler
# required for cargo fetch
COPY policy-controller policy-controller
RUN cargo new policy-test --lib
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    cargo fetch
ARG TARGETARCH
RUN --mount=type=cache,target=target \
    --mount=type=cache,target=/usr/local/cargo/registry \
    target=$(case "$TARGETARCH" in \
        amd64) echo x86_64-unknown-linux-musl ;; \
        arm64) echo aarch64-unknown-linux-musl ;; \
        arm) echo armv7-unknown-linux-musleabihf ;; \
        *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac) && \
    just-cargo profile=$BUILD_TYPE target=$target build --package=ns-labeler && \
    mv "target/$target/$BUILD_TYPE/ns-labeler" /tmp/

FROM scratch as runtime
COPY --from=builder /tmp/ns-labeler /bin/
ENTRYPOINT ["/bin/ns-labeler"]
