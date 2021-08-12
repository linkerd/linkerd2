ARG RUST_IMAGE=docker.io/library/rust:1.54.0-buster
ARG RUNTIME_IMAGE=gcr.io/distroless/cc:nonroot

# Builds the controller binary.
FROM $RUST_IMAGE as build
ARG TARGETARCH
WORKDIR /build
COPY . /build
RUN target=$(rustup show | sed -n 's/^Default host: \(.*\)/\1/p') ; \
    cargo build --locked --release --target="$target" --package=linkerd-policy-controller && \
    mv "target/${target}/release/linkerd-policy-controller" /tmp/

# Creates a minimal runtime image with the controller binary.
FROM $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-policy-controller /bin/
ENTRYPOINT ["/bin/linkerd-policy-controller"]
