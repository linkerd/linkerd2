# The bin image contains binaries for all platforms.
FROM gcr.io/runconduit/cli-bin:latest as build

# We create a new minimal image with the linux binary installed.
FROM gcr.io/runconduit/base:2017-10-30.01

RUN mkdir -p /usr/local/bin
COPY --from=build /out/conduit-linux /usr/local/bin/conduit

ENV PATH=$PATH:/usr/local/bin
ENTRYPOINT ["/usr/local/bin/conduit"]
