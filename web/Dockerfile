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

COPY web/app $PACKAGEDIR
COPY bin/web $ROOT/bin/web
WORKDIR $PACKAGEDIR

# node dependencies
RUN $ROOT/bin/web setup --pure-lockfile

# frontend assets
# set the env to production *after* yarn has done an install, to make sure all
# libraries required for building are included.
ENV NODE_ENV production
RUN $ROOT/bin/web build

## compile go server
FROM gcr.io/linkerd-io/go-deps:6a07271e as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY web web
COPY controller controller
COPY pkg pkg

RUN CGO_ENABLED=0 GOOS=linux go build -o web/web ./web

## package it all up
FROM gcr.io/linkerd-io/base:2017-10-30.01
COPY --from=golang /go/src/github.com/linkerd/linkerd2/web .
RUN mkdir -p ./dist
COPY --from=webpack-bundle /go/src/github.com/linkerd/linkerd2/web/app/dist ./dist

ARG LINKERD_VERSION
ENV LINKERD_CONTAINER_VERSION_OVERRIDE=${LINKERD_VERSION}

ENTRYPOINT ["./web"]
