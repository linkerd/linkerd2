## bundle web assets
FROM node:6.7.0 as webpack-bundle
RUN curl -o- -L https://yarnpkg.com/install.sh | bash
ENV GOPATH /go
ENV PACKAGE github.com/runconduit/conduit/web/app
ENV PACKAGEDIR $GOPATH/src/$PACKAGE
COPY web/app $PACKAGEDIR
WORKDIR $PACKAGEDIR
# node dependencies
RUN $HOME/.yarn/bin/yarn install --pure-lockfile
# frontend assets
RUN $HOME/.yarn/bin/yarn webpack

## compile go server
FROM gcr.io/runconduit/go-deps:e258aef9 as golang
WORKDIR /go/src/github.com/runconduit/conduit
COPY web web
COPY controller controller
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o web/web ./web

## package it all up
FROM gcr.io/runconduit/base:2017-10-30.01
COPY --from=golang /go/src/github.com/runconduit/conduit/web .
RUN mkdir -p ./dist
COPY --from=webpack-bundle /go/src/github.com/runconduit/conduit/web/app/dist ./dist
ENTRYPOINT ["./web"]
