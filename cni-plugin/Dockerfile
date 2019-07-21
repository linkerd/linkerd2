ARG RUNTIME_IMAGE
ARG GOARCH

## compile cni-plugin utility
FROM gcr.io/linkerd-io/go-deps:4c8f4294 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY pkg pkg
COPY controller controller
COPY cni-plugin cni-plugin
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -o /go/bin/linkerd-cni -v ./cni-plugin/

FROM $RUNTIME_IMAGE
WORKDIR /linkerd
RUN curl -kL -o $(which jq) https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64
COPY --from=golang /go/bin/linkerd-cni /opt/cni/bin/
COPY LICENSE .
COPY cni-plugin/deployment/scripts/install-cni.sh .
COPY cni-plugin/deployment/linkerd-cni.conf.default .
COPY cni-plugin/deployment/scripts/filter.jq .
ENV PATH=/linkerd:/opt/cni/bin:$PATH
CMD ["install-cni.sh"]
