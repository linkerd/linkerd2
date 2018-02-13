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

## package it all up
FROM gcr.io/runconduit/base:2017-10-30.01
ENV SOURCE_DIR=target/go/linux_amd64_cgo
COPY ${SOURCE_DIR}/web .
RUN chmod +x ./web
RUN mkdir -p ./dist
COPY --from=webpack-bundle /go/src/github.com/runconduit/conduit/web/app/dist ./dist
ENTRYPOINT ["./web"]
