## compile sidecar-injection utility
FROM gcr.io/linkerd-io/go-deps:3bac64dd as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY ./sidecar-injection ./sidecar-injection
RUN CGO_ENABLED=0 GOOS=linux go install -v ./sidecar-injection/

## package runtime
FROM ubuntu:16.04
COPY --from=golang /go/bin/sidecar-injection /usr/local/bin/sidecar-injection
ENTRYPOINT ["/usr/local/bin/sidecar-injection"]
