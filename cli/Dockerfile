ARG BUILDPLATFORM=linux/amd64

# Precompile key slow-to-build dependencies
FROM --platform=$BUILDPLATFORM golang:1.19.0-alpine as go-deps
WORKDIR /linkerd-build
COPY go.mod go.sum ./
COPY bin/install-deps bin/
RUN go mod download
RUN ./bin/install-deps

## compile binaries
FROM go-deps as go-gen
WORKDIR /linkerd-build
COPY cli cli
COPY charts charts
COPY jaeger jaeger
COPY multicluster multicluster
COPY viz viz

COPY controller/k8s controller/k8s
COPY controller/api controller/api
COPY controller/gen controller/gen
COPY pkg pkg

# Generate static templates
# TODO: `go generate` does not honor -mod=readonly
RUN go generate -mod=readonly ./pkg/charts/static
RUN go generate -mod=readonly ./jaeger/static
RUN go generate -mod=readonly ./multicluster/static
RUN go generate -mod=readonly ./viz/static

RUN mkdir -p /out

FROM go-gen as linux-amd64-full
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/linkerd -tags prod -mod=readonly -ldflags "-s -w" ./cli
ARG LINKERD_VERSION
ENV GO_LDFLAGS="-s -w -X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/linkerd -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen as linux-arm64-full
RUN ./bin/install-deps arm64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /out/linkerd -tags prod -mod=readonly -ldflags "-s -w" ./cli
ARG LINKERD_VERSION
ENV GO_LDFLAGS="-s -w -X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /out/linkerd -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen as linux-arm-full
RUN ./bin/install-deps arm
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -o /out/linkerd -tags prod -mod=readonly -ldflags "-s -w" ./cli
ARG LINKERD_VERSION
ENV GO_LDFLAGS="-s -w -X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -o /out/linkerd -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen as darwin-full
RUN CGO_ENABLED=0 GOOS=darwin go build -o /out/linkerd -tags prod -mod=readonly -ldflags "-s -w" ./cli
ARG LINKERD_VERSION
ENV GO_LDFLAGS="-s -w -X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}"
RUN CGO_ENABLED=0 GOOS=darwin go build -o /out/linkerd -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen as darwin-arm64-full
RUN CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /out/linkerd -tags prod -mod=readonly -ldflags "-s -w" ./cli
ARG LINKERD_VERSION
ENV GO_LDFLAGS="-s -w -X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}"
RUN CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /out/linkerd -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen as windows-full
RUN CGO_ENABLED=0 GOOS=windows go build -o /out/linkerd -tags prod -mod=readonly -ldflags "-s -w" ./cli
ARG LINKERD_VERSION
ENV GO_LDFLAGS="-s -w -X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}"
RUN CGO_ENABLED=0 GOOS=windows go build -o /out/linkerd -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

#
# bin/docker-build* will use any of the following targets depending on the
# value of DOCKER_TARGET, which if it's not set, will be set automatically
# depending on the host's OS and arch.
#

FROM scratch as linux-amd64
COPY LICENSE /linkerd/LICENSE
COPY --from=linux-amd64-full /out/linkerd /out/linkerd-linux-amd64
# `ENTRYPOINT` prevents `docker build` from otherwise failing with "Error
# response from daemon: No command specified."
ENTRYPOINT ["/out/linkerd-linux-amd64"]

FROM scratch as linux-arm64
COPY LICENSE /linkerd/LICENSE
COPY --from=linux-arm64-full /out/linkerd /out/linkerd-linux-arm64
ENTRYPOINT ["/out/linkerd-linux-arm64"]

FROM scratch as linux-arm
COPY LICENSE /linkerd/LICENSE
COPY --from=linux-arm64-full /out/linkerd /out/linkerd-linux-arm
ENTRYPOINT ["/out/linkerd-linux-arm"]

FROM scratch as darwin
COPY LICENSE /linkerd/LICENSE
COPY --from=darwin-full /out/linkerd /out/linkerd-darwin
ENTRYPOINT ["/out/linkerd-darwin"]

FROM scratch as darwin-arm64
COPY LICENSE /linkerd/LICENSE
COPY --from=darwin-arm64-full /out/linkerd /out/linkerd-darwin-arm64
ENTRYPOINT ["/out/linkerd-darwin-arm64"]

FROM scratch as windows
COPY LICENSE /linkerd/LICENSE
COPY --from=windows-full /out/linkerd /out/linkerd-windows
ENTRYPOINT ["/out/linkerd-windows"]

FROM scratch as multi-arch
COPY LICENSE /linkerd/LICENSE
COPY --from=linux-amd64-full /out/linkerd /out/linkerd-linux-amd64
COPY --from=linux-arm64-full /out/linkerd /out/linkerd-linux-arm64
COPY --from=linux-arm-full /out/linkerd /out/linkerd-linux-arm
COPY --from=darwin-full /out/linkerd /out/linkerd-darwin
COPY --from=darwin-arm64-full /out/linkerd /out/linkerd-darwin-arm64
COPY --from=windows-full /out/linkerd /out/linkerd-windows
ENTRYPOINT ["/out/linkerd-linux-amd64"]
