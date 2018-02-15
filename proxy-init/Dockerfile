## package runtime
FROM gcr.io/runconduit/base:2017-10-30.01
ENV SOURCE_DIR=target/go/linux_amd64_cgo
COPY ${SOURCE_DIR}/proxy-init /usr/local/bin/conduit-proxy-init
RUN chmod +x /usr/local/bin/conduit-proxy-init
ENTRYPOINT ["/usr/local/bin/conduit-proxy-init"]
