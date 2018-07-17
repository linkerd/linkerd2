## compile controller services
FROM gcr.io/linkerd-io/go-deps:766a0983 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2

COPY controller/gen controller/gen
COPY pkg pkg
RUN CGO_ENABLED=0 GOOS=linux go install ./pkg/...

COPY controller controller
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/linkerd-controller ./controller/

## package runtime
FROM scratch
ENV PATH=$PATH:/go/bin
COPY --from=golang /go/bin /go/bin

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}
