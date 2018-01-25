## compile controller services
FROM gcr.io/runconduit/go-deps:dac3fae6 as golang
WORKDIR /go/src/github.com/runconduit/conduit
COPY controller controller
COPY pkg pkg
ARG CONDUIT_VERSION
RUN sed -i "s/unknown/$CONDUIT_VERSION/" pkg/version/version.go
# use `install` so that we produce multiple binaries
RUN CGO_ENABLED=0 GOOS=linux go install -a -installsuffix cgo ./controller/cmd/...

## package runtime
FROM gcr.io/runconduit/base:2017-10-30.01
RUN mkdir /go
ENV PATH=$PATH:/go/bin
COPY --from=golang /go/bin /go/bin
