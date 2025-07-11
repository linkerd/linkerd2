ARG BUILDPLATFORM=linux/amd64

# Precompile key slow-to-build dependencies
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS go-deps
WORKDIR /linkerd-build
COPY go.mod go.sum ./
COPY bin/install-deps bin/
RUN go mod download
ARG TARGETARCH
RUN ./bin/install-deps $TARGETARCH

## compile controller service
FROM go-deps AS golang
WORKDIR /linkerd-build
COPY controller/gen controller/gen
COPY pkg pkg
COPY charts charts
COPY controller controller
COPY charts/patch charts/patch
COPY charts/partials charts/partials
COPY multicluster multicluster

ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /out/controller -tags prod -mod=readonly -ldflags "-s -w" ./controller/cmd

## package runtime
FROM scratch
LABEL org.opencontainers.image.source=https://github.com/linkerd/linkerd2
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /out/controller /controller
# for heartbeat (https://versioncheck.linkerd.io/version.json)
COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}
ENTRYPOINT ["/controller"]
