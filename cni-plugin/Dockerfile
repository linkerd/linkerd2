## compile cni-plugin utility
FROM gcr.io/linkerd-io/go-deps:7db9daa2 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2
COPY proxy-init proxy-init
COPY pkg pkg
COPY controller controller
COPY cni-plugin cni-plugin
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/linkerd-cni -v ./cni-plugin/

FROM alpine:3.8
RUN apk --update add bash jq && \
      rm -rf /var/lib/apt/lists/* && \
      rm /var/cache/apk/*
WORKDIR /linkerd
COPY --from=golang /go/bin/linkerd-cni /opt/cni/bin/
COPY LICENSE .
COPY cni-plugin/deployment/scripts/install-cni.sh .
COPY cni-plugin/deployment/linkerd-cni.conf.default .
COPY cni-plugin/deployment/scripts/filter.jq .
ENV PATH=/linkerd:/opt/cni/bin:$PATH
CMD ["install-cni.sh"]
