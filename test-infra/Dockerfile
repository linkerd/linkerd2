FROM gcr.io/linkerd-io/base:2019-02-19.01

# TODO: move at least ca-certificates into Dockerfile-base
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      build-essential \
      ca-certificates \
      git \
    && rm -rf /var/lib/apt/lists/*

# should match Dockerfile-base Debian version
ENV DEBIAN_VERSION debian-stretch
# should match l5d-build.yaml library/docker
ENV DOCKER_VERSION 18.09.6~3-0
ENV GO_VERSION 1.11.5
ENV KUBECTL_VERSION v1.14.3
ENV KIND_VERSION v0.3.0

RUN curl -Lo /tmp/docker.deb https://download.docker.com/linux/debian/dists/stretch/pool/stable/amd64/docker-ce-cli_${DOCKER_VERSION}~${DEBIAN_VERSION}_amd64.deb \
    && dpkg -i /tmp/docker.deb \
    && rm -rf /tmp/docker.deb

RUN curl -Lo /tmp/go.tar.gz https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm -rf /tmp/go.tar.gz
ENV GOPATH /go
ENV PATH $PATH:/usr/local/go/bin

RUN curl -Lo /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl
RUN chmod +x /usr/local/bin/kubectl

RUN curl -Lo /usr/local/bin/kind https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-amd64
RUN chmod +x /usr/local/bin/kind

COPY l5d-builder.sh /usr/local/bin/

ENTRYPOINT ["l5d-builder.sh"]
CMD []

