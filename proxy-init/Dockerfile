## compile proxy-init utility
FROM gcr.io/linkerd-io/go-deps:cdba5b70 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY ./proxy-init ./proxy-init
RUN CGO_ENABLED=0 GOOS=linux go install -v ./proxy-init/

## package runtime
FROM gcr.io/linkerd-io/base:2019-02-19.01
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /go/bin/proxy-init /usr/local/bin/proxy-init
ENTRYPOINT ["/usr/local/bin/proxy-init"]
