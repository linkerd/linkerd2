## compile cni-plugin utility
FROM gcr.io/linkerd-io/go-deps:7db9daa2 as golang
COPY ./proxy-init/cmd /go/src/github.com/linkerd/linkerd2/proxy-init/cmd
COPY ./proxy-init/iptables /go/src/github.com/linkerd/linkerd2/proxy-init/iptables
COPY ./pkg/k8s /go/src/github.com/linkerd/linkerd2/pkg/k8s
COPY ./pkg/version /go/src/github.com/linkerd/linkerd2/pkg/version
COPY ./controller/gen/public /go/src/github.com/linkerd/linkerd2/controller/gen/public
COPY ./controller/gen/common/healthcheck /go/src/github.com/linkerd/linkerd2/controller/gen/common/healthcheck
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY ./cni-plugin ./cni-plugin
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/linkerd-cni -v ./cni-plugin/

FROM alpine:3.8
LABEL description="linkerd CNI plugin installer."
# add bash to allow install-cni.sh to trap process signals
RUN apk --update add bash jq && \
    rm -rf /var/lib/apt/lists/* && \
    rm /var/cache/apk/*

WORKDIR /go/src/github.com/linkerd/linkerd2
COPY --from=golang /go/bin/linkerd-cni /opt/cni/bin/
COPY cni-plugin/deployment/scripts/install-cni.sh /install-cni.sh
COPY cni-plugin/deployment/linkerd-cni.conf.default /linkerd-cni.conf.default
COPY cni-plugin/deployment/scripts/filter.jq /filter.jq

ENV PATH=$PATH:/opt/cni/bin
CMD ["install-cni.sh"]
