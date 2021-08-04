ARG RUST_IMAGE=docker.io/library/rust:1.54.0-buster
ARG RUNTIME_IMAGE=gcr.io/distroless/cc:nonroot

# Build and installs the cargo-chef to be shared to later layers.
#
# cargo-chef stubs out the workspace so we can build the project's dependencies without building the
# controller:
FROM $RUST_IMAGE as chef
ARG TARGETARCH
RUN cargo install cargo-chef \
    --target="$(rustup show | sed -n 's/^Default host: \(.*\)/\1/p')"

# Generates a manifest of dependencies in recipe.json so the dependencies can be built without
# having to copy source files/directories.
FROM $RUST_IMAGE as recipe
ARG TARGETARCH
COPY --from=chef /usr/local/cargo /usr/local/cargo
WORKDIR /build
COPY . /build
RUN cargo chef prepare

# Stubs the workspace so we can build the project's dependencies without building the application.
FROM $RUST_IMAGE as deps
COPY --from=chef /usr/local/cargo /usr/local/cargo
WORKDIR /build
COPY --from=recipe /build/recipe.json recipe.json
RUN target=$(rustup show | sed -n 's/^Default host: \(.*\)/\1/p') ; \
    cargo chef cook --release  --target="$target" --package=linkerd-policy-controller

# Builds the controller binary.
FROM $RUST_IMAGE as build
ARG TARGETARCH
COPY --from=chef /usr/local/cargo /usr/local/cargo
COPY --from=deps /build/target target
WORKDIR /build
COPY . /build
RUN target=$(rustup show | sed -n 's/^Default host: \(.*\)/\1/p') ; \
    cargo build --frozen --release --target="$target" --package=linkerd-policy-controller && \
    mv "target/${target}/release/linkerd-policy-controller" /tmp/

# Creates a minimal runtime image with the controller binary.
FROM $RUNTIME_IMAGE
COPY --from=build /tmp/linkerd-policy-controller /usr/local/bin
ENTRYPOINT ["/usr/local/bin/linkerd-policy-controller"]
