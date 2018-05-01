## compile controller services
FROM gcr.io/runconduit/go-deps:e7d5fcae as golang
ARG CONDUIT_VERSION
WORKDIR /go/src/github.com/runconduit/conduit
COPY controller/gen controller/gen
COPY pkg pkg
RUN CGO_ENABLED=0 GOOS=linux go install -installsuffix cgo -ldflags "-X github.com/runconduit/conduit/pkg/version.Version=${CONDUIT_VERSION}" ./pkg/...
COPY controller controller
# use `install` so that we produce multiple binaries
RUN CGO_ENABLED=0 GOOS=linux go install -installsuffix cgo -ldflags "-X github.com/runconduit/conduit/pkg/version.Version=${CONDUIT_VERSION}" ./controller/cmd/...

## package runtime
FROM scratch
ENV PATH=$PATH:/go/bin
COPY --from=golang /go/bin /go/bin
