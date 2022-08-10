ARG GO_VERSION=1.18
ARG RUST_TOOLCHAIN=1.62.1

##
## Base
##

FROM docker.io/debian:stable-slim as base
RUN apt update && apt upgrade -y --autoremove \
    && apt install -y \
        curl \
        file \
        git \
        jo \
        jq \
        time \
        unzip \
        xz-utils \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
COPY bin/scurl /usr/local/bin/scurl

FROM base as just
ARG JUST_VERSION=1.2.0
RUN url="https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-x86_64-unknown-linux-musl.tar.gz" ; \
    scurl "$url" | tar zvxf - -C /usr/local/bin just

FROM base as protoc
ARG PROTOC_VERSION=v3.20.1
RUN url="https://github.com/google/protobuf/releases/download/$PROTOC_VERSION/protoc-${PROTOC_VERSION#v}-linux-$(uname -m).zip" ; \
    cd $(mktemp -d) && \
    scurl -o protoc.zip  "$url" && \
    unzip protoc.zip bin/protoc include/** && \
    mv bin/protoc /usr/local/bin/protoc && \
    chmod +x /usr/local/bin/protoc && \
    mkdir -p /usr/local/include && \
    mv include/google /usr/local/include/

FROM base as yq
ARG YQ_VERSION=v4.25.1
RUN url="https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" ; \
    scurl -o /usr/local/bin/yq "$url" && chmod +x /usr/local/bin/yq

##
## Rust image
##

FROM base as cargo-action-fmt
ARG CARGO_ACTION_FMT_VERSION=1.0.2
RUN url="https://github.com/olix0r/cargo-action-fmt/releases/download/release%2Fv${CARGO_ACTION_FMT_VERSION}/cargo-action-fmt-x86_64-unknown-linux-gnu" ; \
    scurl -o /usr/local/bin/cargo-action-fmt "$url" && chmod +x /usr/local/bin/cargo-action-fmt

FROM base as cargo-deny
ARG CARGO_DENY_VERSION=0.12.1
RUN url="https://github.com/EmbarkStudios/cargo-deny/releases/download/${CARGO_DENY_VERSION}/cargo-deny-${CARGO_DENY_VERSION}-x86_64-unknown-linux-musl.tar.gz" ; \
    scurl "$url" | tar zvxf - --strip-components=1 -C /usr/local/bin "cargo-deny-${CARGO_DENY_VERSION}-x86_64-unknown-linux-musl/cargo-deny"

FROM base as cargo-nextest
ARG NEXTEST_VERSION=0.9.24
RUN url="https://github.com/nextest-rs/nextest/releases/download/cargo-nextest-${NEXTEST_VERSION}/cargo-nextest-${NEXTEST_VERSION}-x86_64-unknown-linux-gnu.tar.gz" ; \
    scurl "$url" | tar zvxf - -C /usr/local/bin cargo-nextest

FROM base as cargo-tarpaulin
ARG CARGO_TARPAULIN_VERSION=0.20.1
RUN url="https://github.com/xd009642/tarpaulin/releases/download/${CARGO_TARPAULIN_VERSION}/cargo-tarpaulin-${CARGO_TARPAULIN_VERSION}-travis.tar.gz" ; \
    scurl "$url" | tar xzvf - -C /usr/local/bin cargo-tarpaulin

FROM docker.io/rust:${RUST_TOOLCHAIN}-slim as rust
RUN rustup component add clippy rustfmt
RUN apt update && apt upgrade -y --autoremove \
    && apt install -y \
        clang \
        cmake \
        curl \
        git \
        jo \
        jq \
        libssl-dev \
        pkg-config \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
COPY --from=cargo-action-fmt /usr/local/bin/cargo-action-fmt /usr/local/cargo/bin/
COPY --from=cargo-deny /usr/local/bin/cargo-deny /usr/local/cargo/bin/
COPY --from=cargo-nextest /usr/local/bin/cargo-nextest /usr/local/cargo/bin/
COPY --from=cargo-tarpaulin /usr/local/bin/cargo-tarpaulin /usr/local/cargo/bin/
COPY --from=just /usr/local/bin/just /usr/local/bin/
COPY --from=protoc /usr/local/bin/protoc /usr/local/bin/
COPY --from=protoc /usr/local/include/google /usr/local/include/google
COPY --from=yq /usr/local/bin/yq /usr/local/bin/
COPY bin/scurl /usr/local/bin/scurl
ENV USER=root
ENV PROTOC_NO_VENDOR=1
ENV PROTOC=/usr/local/bin/protoc
ENV PROTOC_INCLUDE=/usr/local/include

##
## Go image
##

FROM docker.io/golang:${GO_VERSION} as go
RUN apt update && apt upgrade -y --autoremove \
    && apt install -y \
        curl \
        file \
        jq \
        time \
        unzip \
        xz-utils \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN for p in \
    github.com/cweill/gotests/gotests@latest \
    github.com/go-delve/delve/cmd/dlv@latest \
    github.com/golangci/golangci-lint/cmd/golangci-lint@v1.46.2 \
    github.com/fatih/gomodifytags@latest \
    github.com/haya14busa/goplay/cmd/goplay@latest \
    github.com/josharian/impl@latest \
    github.com/ramya-rao-a/go-outline@latest \
    github.com/uudashr/gopkgs/v2/cmd/gopkgs@latest \
    golang.org/x/tools/gopls@latest \
    google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2 \
    google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1 \
    gotest.tools/gotestsum@v0.4.2 \
    ; do go install -mod=readonly "$p" ; done \
    && rm -rf /go/pkg/* /go/src/*
COPY bin/scurl /usr/local/bin/scurl
COPY --from=protoc /usr/local/bin/protoc /usr/local/bin/
COPY --from=protoc /usr/local/include/google /usr/local/include/google
COPY --from=just /usr/local/bin/just /usr/local/bin/
ENV PROTOC_NO_VENDOR=1
ENV PROTOC=/usr/local/bin/protoc
ENV PROTOC_INCLUDE=/usr/local/include

##
## Kubernetes tools
##

FROM base as k8s

ARG KUBECTL_VERSION=v1.24.3
RUN url="https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" ; \
    scurl -o /usr/local/bin/kubectl "$url" && chmod +x /usr/local/bin/kubectl

ARG K3D_VERSION=v5.4.4
RUN url="https://raw.githubusercontent.com/rancher/k3d/$K3D_VERSION/install.sh" ; \
    scurl "$url" | USE_SUDO=false K3D_INSTALL_DIR=/usr/local/bin bash

ARG HELM_VERSION=v3.9.2
RUN url="https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" ; \
    scurl "$url" | tar xzvf - --strip-components=1 -C /usr/local/bin linux-amd64/helm

ARG HELM_DOCS_VERSION=v1.11.0
RUN url="https://github.com/norwoodj/helm-docs/releases/download/$HELM_DOCS_VERSION/helm-docs_${HELM_DOCS_VERSION#v}_Linux_x86_64.tar.gz" ; \
    scurl "$url" | tar xzvf - -C /usr/local/bin helm-docs

##
## Other tools
##

FROM base as checksec
ARG CHECKSEC_VERSION=2.5.0
RUN url="https://raw.githubusercontent.com/slimm609/checksec.sh/${CHECKSEC_VERSION}/checksec" ; \
    scurl -o /usr/local/bin/checksec "$url" && chmod 755 /usr/local/bin/checksec

FROM base as shellcheck
ARG SHELLCHECK_VERSION=v0.8.0
RUN url="https://github.com/koalaman/shellcheck/releases/download/${SHELLCHECK_VERSION}/shellcheck-${SHELLCHECK_VERSION}.linux.x86_64.tar.xz" ; \
    scurl "$url" | tar xJvf - --strip-components=1 -C /usr/local/bin "shellcheck-${SHELLCHECK_VERSION}/shellcheck"

FROM shellcheck as actionlint
ARG ACTIONLINT_VERSION=v1.6.15
RUN url="https://github.com/rhysd/actionlint/releases/download/${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION#v}_linux_amd64.tar.gz" ; \
    scurl "$url" | tar xzvf - -C /usr/local/bin actionlint

FROM docker.io/rust:${RUST_TOOLCHAIN}-slim as json5-to-json
RUN cargo install json5-to-json

##
## Tools: Everything needed for a development environment, minus non-root settings.
##

FROM base as tools
COPY --from=actionlint /usr/local/bin/actionlint /usr/local/bin/
COPY --from=checksec /usr/local/bin/checksec /usr/local/bin/che
COPY --from=json5-to-json /usr/local/cargo/bin/json5-to-json /usr/local/bin/
COPY --from=just /usr/local/bin/just /usr/local/bin/
COPY --from=k8s /usr/local/bin/helm /usr/local/bin/
COPY --from=k8s /usr/local/bin/helm-docs /usr/local/bin/
COPY --from=k8s /usr/local/bin/k3d /usr/local/bin/
COPY --from=k8s /usr/local/bin/kubectl /usr/local/bin/
COPY --from=protoc /usr/local/bin/protoc /usr/local/bin/
COPY --from=protoc /usr/local/include/google /usr/local/include/google
COPY --from=shellcheck /usr/local/bin/shellcheck /usr/local/bin/
COPY --from=yq /usr/local/bin/yq /usr/local/bin/
ENV PROTOC_NO_VENDOR=1
ENV PROTOC=/usr/local/bin/protoc
ENV PROTOC_INCLUDE=/usr/local/include

##
## Runtime
##

FROM docker.io/debian:stable as runtime
RUN apt update && apt upgrade -y --autoremove \
    && apt install -y \
        clang curl \
        cmake \
        file \
        jo \
        jq \
        libssl-dev \
        locales \
        lsb-release \
        npm \
        pkg-config \
        sudo \
        time \
        unzip \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN sed -i 's/^# *\(en_US.UTF-8\)/\1/' /etc/locale.gen && locale-gen
RUN (echo "LC_ALL=en_US.UTF-8" \
    && echo "LANGUAGE=en_US.UTF-8") >/etc/default/locale

ARG MARKDOWNLINT_VERSION=0.4.0
RUN npm install "markdownlint-cli2@${MARKDOWNLINT_VERSION}" --global

ENV GOPATH=/go
COPY --from=go /go/bin $GOPATH/bin
COPY --from=go /usr/local/go /usr/local/go
RUN chmod 777 /go
ENV PATH=/usr/local/go/bin:$GOPATH/bin:$PATH

ENV CARGO_HOME=/usr/local/cargo
ENV RUSTUP_HOME=/usr/local/rustup
COPY --from=rust $CARGO_HOME $CARGO_HOME
COPY --from=rust $RUSTUP_HOME $RUSTUP_HOME
ENV PATH=$CARGO_HOME/bin:$PATH
RUN rustup component add rust-analysis rust-std

COPY --from=tools /usr/local/bin/* /usr/local/bin/
COPY --from=protoc /usr/local/include/google /usr/local/include/google

ENV PROTOC_NO_VENDOR=1
ENV PROTOC=/usr/local/bin/protoc
ENV PROTOC_INCLUDE=/usr/local/include

ENV DOCKER_BUILDKIT=1
RUN groupadd --gid=1000 code \
    && useradd --create-home --uid=1000 --gid=1000 code \
    && echo "code ALL=(root) NOPASSWD:ALL" >/etc/sudoers.d/code \
    && chmod 0440 /etc/sudoers.d/code \
    && scurl https://raw.githubusercontent.com/microsoft/vscode-dev-containers/main/script-library/docker-debian.sh | bash -s \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN chmod 777 $CARGO_HOME $RUSTUP_HOME
ENV USER=code
ENV HOME=/home/code
USER code

ENTRYPOINT ["/usr/local/share/docker-init.sh"]
CMD ["sleep", "infinity"]
