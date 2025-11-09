# Build stage
FROM docker.io/rust:1.90-bookworm AS build

WORKDIR /build

# Copy workspace manifest
COPY Cargo.toml ./

# Copy binary crate
COPY dst-load-controller ./dst-load-controller

# Build the binary
RUN cargo build --release --bin dst-load-controller

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /build/target/release/dst-load-controller /usr/local/bin/dst-load-controller

ENTRYPOINT ["/usr/local/bin/dst-load-controller"]
