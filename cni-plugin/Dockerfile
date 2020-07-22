# Precompile key slow-to-build dependencies
FROM golang:1.14.2-alpine as go-deps
WORKDIR /linkerd-build
COPY go.mod go.sum ./
COPY bin/install-deps bin/
RUN go mod download
RUN ./bin/install-deps

## compile cni-plugin utility
FROM go-deps as golang
WORKDIR /linkerd-build
COPY pkg pkg
COPY controller controller
COPY cni-plugin cni-plugin
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/linkerd-cni -v -mod=readonly ./cni-plugin/

FROM debian:buster-20200514-slim
WORKDIR /linkerd
RUN apt-get update && apt-get install -y --no-install-recommends \
    iptables \
    jq && \
    rm -rf /var/lib/apt/lists/*

# We still rely on old iptables-legacy syntax.
RUN update-alternatives --set iptables /usr/sbin/iptables-legacy \
    && update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy

COPY --from=golang /go/bin/linkerd-cni /opt/cni/bin/
COPY LICENSE .
COPY cni-plugin/deployment/scripts/install-cni.sh .
COPY cni-plugin/deployment/linkerd-cni.conf.default .
COPY cni-plugin/deployment/scripts/filter.jq .
ENV PATH=/linkerd:/opt/cni/bin:$PATH
CMD ["install-cni.sh"]
