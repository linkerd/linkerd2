## compile proxy-init utility
FROM gcr.io/runconduit/go-deps:e7d5fcae as golang
WORKDIR /go/src/github.com/runconduit/conduit
COPY ./proxy-init ./proxy-init
RUN CGO_ENABLED=0 GOOS=linux go install -v -installsuffix cgo ./proxy-init/

## package runtime
FROM gcr.io/runconduit/base:2017-10-30.01
COPY --from=golang /go/bin/proxy-init /usr/local/bin/conduit-proxy-init
ENTRYPOINT ["/usr/local/bin/conduit-proxy-init"]
