ARG BUILDPLATFORM=linux/amd64

FROM --platform=$BUILDPLATFORM docker.io/curlimages/curl:7.75.0  as await
WORKDIR /tmp
ARG LINKERD_AWAIT_VERSION=v0.2.1
ARG TARGETARCH
RUN curl -fsSvLo linkerd-await https://github.com/olix0r/linkerd-await/releases/download/release%2F${LINKERD_AWAIT_VERSION}/linkerd-await-${LINKERD_AWAIT_VERSION}-${TARGETARCH} && chmod +x linkerd-await

# Precompile key slow-to-build dependencies
FROM --platform=$BUILDPLATFORM golang:1.14.15-alpine as go-deps
WORKDIR /linkerd-build
COPY go.mod go.sum ./
COPY bin/install-deps bin/
RUN go mod download
ARG TARGETARCH
RUN ./bin/install-deps $TARGETARCH

## compile controller service
FROM go-deps as golang
WORKDIR /linkerd-build
COPY jaeger jaeger
COPY controller/gen controller/gen
COPY pkg pkg
COPY controller controller
COPY charts/partials charts/partials

ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /out/injector -tags prod -mod=readonly -ldflags "-s -w" ./jaeger/injector/cmd

## package runtime
FROM scratch
ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}
COPY --from=await /tmp/linkerd-await /
COPY --from=golang /out/injector /injector

ENTRYPOINT ["/linkerd-await", "--", "/injector"]
