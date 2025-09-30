ARG BUILDPLATFORM=linux/amd64

# Precompile key slow-to-build dependencies
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS go-deps
WORKDIR /linkerd-build
COPY go.mod go.sum ./
RUN go mod download

## compile binaries
FROM go-deps AS go-gen
WORKDIR /linkerd-build
COPY cli cli
COPY charts charts
COPY multicluster multicluster
COPY viz viz

COPY controller/k8s controller/k8s
COPY controller/api controller/api
COPY controller/gen controller/gen
COPY pkg pkg

RUN mkdir -p /out

ARG LINKERD_VERSION="dev-0.0.0"
ENV CGO_ENABLED=0
ENV GO_LDFLAGS="-s -w -X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}"

FROM go-gen AS build-linux-amd64
ENV GOOS=linux GOARCH=amd64
ENV BIN=/out/linkerd2-cli-${LINKERD_VERSION}-linux-amd64
RUN go build -o "$BIN" -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen AS build-linux-arm64
ENV GOOS=linux GOARCH=arm64
ENV BIN=/out/linkerd2-cli-${LINKERD_VERSION}-linux-arm64
RUN go build -o "$BIN" -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen AS build-darwin
ENV GOOS=darwin GOARCH=amd64
ENV BIN=/out/linkerd2-cli-${LINKERD_VERSION}-${GOOS}
RUN go build -o "$BIN" -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen AS build-darwin-arm64
ENV GOOS=darwin GOARCH=arm64
ENV BIN=/out/linkerd2-cli-${LINKERD_VERSION}-${GOOS}-${GOARCH}
RUN go build -o "$BIN" -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

FROM go-gen AS build-windows
ENV GOOS=windows GOARCH=amd64
ENV BIN=/out/linkerd2-cli-${LINKERD_VERSION}-${GOOS}.exe
RUN go build -o "$BIN" -tags prod -mod=readonly -ldflags "${GO_LDFLAGS}" ./cli

# Used in CI
FROM scratch AS linux-amd64
COPY --from=build-linux-amd64 /out/* /

FROM scratch AS multi-arch
COPY --from=build-linux-amd64  /out/* /
COPY --from=build-linux-arm64  /out/* /
COPY --from=build-darwin       /out/* /
COPY --from=build-darwin-arm64 /out/* /
COPY --from=build-windows      /out/* /
