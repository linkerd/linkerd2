ARG BUILDPLATFORM=linux/amd64

# Precompile key slow-to-build dependencies
FROM --platform=$BUILDPLATFORM golang:1.16.2-alpine as go-deps
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
COPY --from=golang /out/injector /injector

ENTRYPOINT ["/injector"]
