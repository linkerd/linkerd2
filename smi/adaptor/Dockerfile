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
COPY smi smi
COPY controller/gen controller/gen
COPY pkg pkg
COPY controller controller

ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /out/smi-adaptor -tags prod -mod=readonly -ldflags "-s -w" ./smi/cmd/adaptor

## package runtime
FROM scratch
ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}
COPY --from=golang /out/smi-adaptor /smi-adaptor

ENTRYPOINT ["/smi-adaptor"]
