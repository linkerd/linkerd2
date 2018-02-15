FROM gcr.io/runconduit/base:2017-10-30.01
RUN mkdir /go
ENV PATH=$PATH:/go/bin
ENV SOURCE_DIR=target/go/linux_amd64_cgo
COPY \
  ${SOURCE_DIR}/destination \
  ${SOURCE_DIR}/proxy-api \
  ${SOURCE_DIR}/public-api \
  ${SOURCE_DIR}/tap \
  ${SOURCE_DIR}/telemetry \
  /go/bin/
RUN chmod +x \
  /go/bin/destination \
  /go/bin/proxy-api \
  /go/bin/proxy-api \
  /go/bin/public-api \
  /go/bin/tap \
  /go/bin/telemetry
