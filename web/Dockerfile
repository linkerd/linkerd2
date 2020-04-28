ARG ARCH=amd64
ARG RUNTIME_IMAGE=$ARCH/debian:buster-20200514-slim
ARG GO_DEPS_IMAGE=gcr.io/linkerd-io/go-deps
ARG GO_DEBS_VERSION=30d997a8

## bundle web assets
FROM node:14-buster as webpack-bundle
RUN curl -o- -L https://yarnpkg.com/install.sh | bash -s -- --version 1.21.1 --network-concurrency 1

ENV PATH /root/.yarn/bin:$PATH
ENV ROOT /linkerd-build
WORKDIR $ROOT

# copy build script
COPY bin/web ./bin/web

# install yarn dependencies
COPY web/app/package.json web/app/yarn.lock ./web/app/
RUN ./bin/web setup install --frozen-lockfile

# build frontend assets
# set the env to production *after* yarn has done an install, to make sure all
# libraries required for building are included.
ENV NODE_ENV production
COPY web/app ./web/app
RUN ./bin/web build

FROM $GO_DEPS_IMAGE:$GO_DEBS_VERSION as golang-amd64
ENV GOARCH=amd64

FROM $GO_DEPS_IMAGE:$GO_DEBS_VERSION as golang-arm32v7
ENV GOARCH=arm
ENV GOARM=7

FROM $GO_DEPS_IMAGE:$GO_DEBS_VERSION as golang-arm64v8
ENV GOARCH=arm64

## compile go server
FROM golang-$ARCH as golang
WORKDIR /linkerd-build
RUN mkdir -p web app
COPY web/main.go web
COPY web/srv web/srv
COPY controller controller
COPY pkg pkg

RUN CGO_ENABLED=0 GOOS=linux go build -mod=readonly -o web/web -ldflags "-s -w" ./web

## package it all up
FROM $RUNTIME_IMAGE
WORKDIR /linkerd

COPY LICENSE .
COPY --from=golang /linkerd-build/web/web .
COPY --from=webpack-bundle /linkerd-build/web/app/dist app/dist
COPY web/templates templates
COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}

ENTRYPOINT ["./web"]
