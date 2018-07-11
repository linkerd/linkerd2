## compile controller services
FROM gcr.io/linkerd-io/go-deps:766a0983 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY controller/gen controller/gen
COPY pkg pkg
COPY controller controller

# Cache a build without version info
RUN CGO_ENABLED=0 GOOS=linux go build ./pkg/...
RUN CGO_ENABLED=0 GOOS=linux go build ./controller/cmd/...

ARG LINKERD_VERSION
# use `install` so that we produce multiple binaries
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags "-X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}" ./pkg/...
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags "-X github.com/linkerd/linkerd2/pkg/version.Version=${LINKERD_VERSION}" ./controller/cmd/...

## package runtime
FROM scratch
ENV PATH=$PATH:/go/bin
COPY --from=golang /go/bin /go/bin
