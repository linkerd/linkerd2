## compile proxy-init utility
<<<<<<< HEAD
FROM gcr.io/runconduit/go-deps:f1df2168 as golang
=======
FROM gcr.io/runconduit/go-deps:7bceac7a as golang
>>>>>>> master
WORKDIR /go/src/github.com/runconduit/conduit
COPY ./proxy-init ./proxy-init
RUN CGO_ENABLED=0 GOOS=linux go install -v -a -installsuffix cgo ./proxy-init/

## package runtime
FROM gcr.io/runconduit/base:2017-10-30.01
COPY --from=golang /go/bin/proxy-init /usr/local/bin/conduit-proxy-init
ENTRYPOINT ["/usr/local/bin/conduit-proxy-init"]
