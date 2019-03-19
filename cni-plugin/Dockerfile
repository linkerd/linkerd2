## compile cni-plugin utility
FROM gcr.io/linkerd-io/go-deps:cdba5b70 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY proxy-init proxy-init
COPY pkg pkg
COPY controller controller
COPY cni-plugin cni-plugin
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/linkerd-cni -v ./cni-plugin/

FROM gcr.io/linkerd-io/base:2019-02-19.01
WORKDIR /linkerd
RUN curl -kL -o $(which jq) https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64
COPY --from=golang /go/bin/linkerd-cni /opt/cni/bin/
COPY LICENSE .
COPY cni-plugin/deployment/scripts/install-cni.sh .
COPY cni-plugin/deployment/linkerd-cni.conf.default .
COPY cni-plugin/deployment/scripts/filter.jq .
ENV PATH=/linkerd:/opt/cni/bin:$PATH
CMD ["install-cni.sh"]
