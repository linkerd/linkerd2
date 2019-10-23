## bundle web assets
FROM node:10 as webpack-bundle
RUN curl -o- -L https://yarnpkg.com/install.sh | bash -s -- --version 1.7.0

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
FROM gcr.io/linkerd-io/go-deps:f4c3ddf4 as golang
WORKDIR /linkerd-build
RUN mkdir -p web
COPY web/main.go web
COPY web/srv web/srv
COPY controller controller
COPY pkg pkg

RUN CGO_ENABLED=0 GOOS=linux go build -mod=readonly -o web/web -ldflags "-s -w" ./web

## package it all up
FROM debian:stretch-20190812-slim
WORKDIR /linkerd

COPY LICENSE .
COPY --from=golang /linkerd-build/web/web .
RUN mkdir -p app
COPY --from=webpack-bundle /linkerd-build/web/app/dist app/dist
COPY web/templates templates

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}

ENTRYPOINT ["./web"]
