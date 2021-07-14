ARG RUST_IMAGE=docker.io/library/rust:1.53.0-buster
ARG RUNTIME_IMAGE=gcr.io/distroless/cc:nonroot

FROM $RUST_IMAGE as build
ARG TARGETARCH
WORKDIR /build
COPY . /build
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.53.0,source=/usr/local/cargo,target=/usr/local/cargo \
    target=$(rustup show | sed -n 's/^Default host: \(.*\)/\1/p') ; \
    cargo build --locked --release --target=$target && \
    mv target/${target}/release/linkerd-policy-controller /tmp

FROM $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-policy-controller /usr/local/bin
ENTRYPOINT ["/usr/local/bin/linkerd-policy-controller"]
