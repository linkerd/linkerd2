# We create a new minimal image with the linux binary installed.
FROM gcr.io/runconduit/base:2017-10-30.01

RUN mkdir -p /usr/local/bin
ENV SOURCE_DIR=target/go/linux_amd64_cgo
COPY ${SOURCE_DIR}/conduit /usr/local/bin/conduit

ENV PATH=$PATH:/usr/local/bin
ENTRYPOINT ["/usr/local/bin/conduit"]
