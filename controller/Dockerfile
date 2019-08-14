## compile controller services
FROM gcr.io/linkerd-io/go-deps:813c9be3 as golang
WORKDIR /linkerd-build
COPY controller/gen controller/gen
COPY pkg pkg
COPY controller controller
COPY charts/patch charts/patch
COPY charts/partials charts/partials

# Generate static templates
# TODO: `go generate` does not honor -mod=readonly
RUN go generate -mod=readonly ./pkg/charts/static

# use `install` so that we produce multiple binaries
RUN CGO_ENABLED=0 GOOS=linux go install -tags prod -mod=readonly ./pkg/...
RUN CGO_ENABLED=0 GOOS=linux go install -tags prod -mod=readonly ./controller/cmd/...

## package runtime
FROM scratch
ENV PATH=$PATH:/go/bin
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /go/bin /go/bin
# for heartbeat (https://versioncheck.linkerd.io/version.json)
COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}
