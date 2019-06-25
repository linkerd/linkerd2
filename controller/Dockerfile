## compile controller services
FROM gcr.io/linkerd-io/go-deps:6f3448b4 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY controller/gen controller/gen
COPY pkg pkg
COPY controller controller

# use `install` so that we produce multiple binaries
RUN CGO_ENABLED=0 GOOS=linux go install ./pkg/...
RUN CGO_ENABLED=0 GOOS=linux go install ./controller/cmd/...

# add trusted certs so the identity service can communicate with aws
FROM alpine:latest as certs
RUN apk --update add ca-certificates

## package runtime
FROM scratch
ENV PATH=$PATH:/go/bin
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /go/bin /go/bin
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}
