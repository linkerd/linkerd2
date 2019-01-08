## compile cni-plugin utility
FROM gcr.io/linkerd-io/go-deps:a077f3a7 as golang
COPY ./proxy-init/cmd /go/src/github.com/linkerd/linkerd2/proxy-init/cmd
COPY ./proxy-init/iptables /go/src/github.com/linkerd/linkerd2/proxy-init/iptables
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY ./cni-plugin ./cni-plugin
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/linkerd2-cni -v ./cni-plugin/

FROM alpine:3.8
LABEL description="linkerd2 CNI plugin installer."
# add bash to allow install-cni.sh to trap process signals
RUN apk --update add bash && \
    rm -rf /var/lib/apt/lists/* && \
    rm /var/cache/apk/*

WORKDIR /go/src/github.com/linkerd/linkerd2
COPY --from=golang /go/bin/linkerd2-cni /opt/cni/bin/
COPY cni-plugin/deployment/scripts/install-cni.sh /install-cni.sh
COPY cni-plugin/deployment/linkerd2-cni.conf.default /linkerd2.conf.tmp

ENV PATH=$PATH:/opt/cni/bin
WORKDIR /opt/cni/bin
CMD ["install-cni.sh"]
