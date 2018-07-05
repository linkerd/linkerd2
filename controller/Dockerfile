## compile controller services
FROM gcr.io/runconduit/go-deps:8b4952a2 as golang
WORKDIR /go/src/github.com/runconduit/conduit
COPY controller/gen controller/gen
COPY pkg pkg
COPY controller controller

# Cache a build without version info
RUN CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo ./pkg/...
RUN CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo ./controller/cmd/...

ARG CONDUIT_VERSION
# use `install` so that we produce multiple binaries
RUN CGO_ENABLED=0 GOOS=linux go install -installsuffix cgo -ldflags "-X github.com/runconduit/conduit/pkg/version.Version=${CONDUIT_VERSION}" ./pkg/...
RUN CGO_ENABLED=0 GOOS=linux go install -installsuffix cgo -ldflags "-X github.com/runconduit/conduit/pkg/version.Version=${CONDUIT_VERSION}" ./controller/cmd/...

## package runtime
FROM scratch
ENV PATH=$PATH:/go/bin
COPY --from=golang /go/bin /go/bin
