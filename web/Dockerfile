## bundle web assets
FROM node:6.7.0 as webpack-bundle
RUN curl -o- -L https://yarnpkg.com/install.sh | bash

ENV HOME /root
ENV PATH $HOME/.yarn/bin:$PATH
ENV GOPATH /go
ENV PROJECT github.com/runconduit/conduit
ENV PACKAGE $PROJECT/web/app
ENV ROOT $GOPATH/src/$PROJECT
ENV PACKAGEDIR $GOPATH/src/$PACKAGE

COPY web/app $PACKAGEDIR
COPY bin $ROOT/bin
WORKDIR $PACKAGEDIR

# node dependencies
RUN $ROOT/bin/web setup --pure-lockfile

# frontend assets
# set the env to production *after* yarn has done an install, to make sure all
# libraries required for building are included.
ENV NODE_ENV production
RUN $ROOT/bin/web build

## compile go server
FROM gcr.io/runconduit/go-deps:69ba71ed as golang
WORKDIR /go/src/github.com/runconduit/conduit
COPY web web
COPY controller controller
COPY pkg pkg

# Cache a build without version info
RUN CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo -o web/web ./web

ARG CONDUIT_VERSION
RUN CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo -o web/web -ldflags "-X github.com/runconduit/conduit/pkg/version.Version=$CONDUIT_VERSION" ./web

## package it all up
FROM gcr.io/runconduit/base:2017-10-30.01
COPY --from=golang /go/src/github.com/runconduit/conduit/web .
RUN mkdir -p ./dist
COPY --from=webpack-bundle /go/src/github.com/runconduit/conduit/web/app/dist ./dist
ENTRYPOINT ["./web"]
