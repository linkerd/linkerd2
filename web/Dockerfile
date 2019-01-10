## bundle web assets
FROM node:10 as webpack-bundle
RUN curl -o- -L https://yarnpkg.com/install.sh | bash -s -- --version 1.7.0

ENV HOME /root
ENV PATH $HOME/.yarn/bin:$PATH
ENV GOPATH /go
ENV PROJECT github.com/linkerd/linkerd2
ENV PACKAGE $PROJECT/web/app
ENV ROOT $GOPATH/src/$PROJECT
ENV PACKAGEDIR $GOPATH/src/$PACKAGE
WORKDIR $PACKAGEDIR

# copy build script
COPY bin/web $ROOT/bin/web

# install yarn dependencies
COPY web/app/package.json web/app/yarn.lock ./
RUN $ROOT/bin/web setup install --frozen-lockfile

# build frontend assets
# set the env to production *after* yarn has done an install, to make sure all
# libraries required for building are included.
ENV NODE_ENV production
COPY web/app .
RUN $ROOT/bin/web build

## compile go server
FROM gcr.io/linkerd-io/go-deps:f95a60fe as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
RUN mkdir -p web
COPY web/main.go web
COPY web/srv web/srv
COPY controller controller
COPY pkg pkg

RUN CGO_ENABLED=0 GOOS=linux go build -o web/web ./web

## package it all up
FROM gcr.io/linkerd-io/base:2017-10-30.01
WORKDIR /linkerd

COPY LICENSE .
COPY --from=golang /go/src/github.com/linkerd/linkerd2/web/web .
RUN mkdir -p app
COPY --from=webpack-bundle /go/src/github.com/linkerd/linkerd2/web/app/dist app/dist
COPY web/templates templates

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}

ENTRYPOINT ["./web"]
