## compile controller service
FROM gcr.io/linkerd-io/go-deps:f4c3ddf4 as golang
WORKDIR /linkerd-build
COPY controller/gen controller/gen
COPY pkg pkg
COPY controller controller
COPY charts/patch charts/patch
COPY charts/partials charts/partials

# Generate static templates
# TODO: `go generate` does not honor -mod=readonly
RUN go generate -mod=readonly ./pkg/charts/static

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/controller -tags prod -mod=readonly -ldflags "-s -w" ./controller/cmd

## package runtime
FROM scratch
ENV PATH=$PATH:/go/bin
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /out/controller /go/bin/controller
# for heartbeat (https://versioncheck.linkerd.io/version.json)
COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}

ENTRYPOINT ["/go/bin/controller"]
