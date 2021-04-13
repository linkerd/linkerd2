ARG BUILDPLATFORM=linux/amd64

# Precompile key slow-to-build dependencies
FROM --platform=$BUILDPLATFORM golang:1.16.2-alpine as go-deps
WORKDIR /linkerd-build
COPY go.mod go.sum ./
COPY bin/install-deps bin/
RUN go mod download
ARG TARGETARCH
RUN ./bin/install-deps $TARGETARCH

## bundle web assets
FROM --platform=$BUILDPLATFORM node:15-buster as webpack-bundle
RUN curl -o- -L https://yarnpkg.com/install.sh | bash -s -- --version 1.22.10 --network-concurrency 1

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

## compile go server
FROM go-deps as golang
WORKDIR /linkerd-build
RUN mkdir -p web
COPY web/main.go web
COPY web/srv web/srv
COPY controller controller
COPY viz/metrics-api viz/metrics-api
COPY viz/pkg viz/pkg
COPY viz/tap/gen/tap viz/tap/gen/tap
COPY viz/tap/pkg viz/tap/pkg
COPY pkg pkg
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -mod=readonly -o web/web -ldflags "-s -w" ./web

## package it all up
FROM scratch
WORKDIR /linkerd

COPY LICENSE .
COPY --from=golang /linkerd-build/web/web .
COPY --from=webpack-bundle /linkerd-build/web/app/dist app/dist
COPY web/templates templates
COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}

ENTRYPOINT ["./web"]
