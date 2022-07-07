ARG GO_VERSION=1.17
ARG RUST_TOOLCHAIN=1.60.0

FROM docker.io/golang:${GO_VERSION}-bullseye as go
ARG GOLANGCI_LINT_VERSION=v1.44.2
RUN for p in \
    github.com/uudashr/gopkgs/v2/cmd/gopkgs@latest \
    github.com/ramya-rao-a/go-outline@latest \
    github.com/cweill/gotests/gotests@latest \
    github.com/fatih/gomodifytags@latest \
    github.com/josharian/impl@latest \
    github.com/haya14busa/goplay/cmd/goplay@latest \
    github.com/go-delve/delve/cmd/dlv@latest \
    github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} \
    golang.org/x/tools/gopls@latest \
    google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2 \
    google.golang.org/protobuf/cmd/protoc-gen-go@v1.28 \
    ; do go install "$p" ; done

FROM docker.io/golang:${GO_VERSION}-bullseye as cargo-deny
ARG CARGO_DENY_VERSION=0.12.1
COPY bin/scurl /usr/local/bin/scurl
RUN scurl "https://github.com/EmbarkStudios/cargo-deny/releases/download/${CARGO_DENY_VERSION}/cargo-deny-${CARGO_DENY_VERSION}-x86_64-unknown-linux-musl.tar.gz" \
    | tar zvxf - --strip-components=1 -C /usr/local/bin "cargo-deny-${CARGO_DENY_VERSION}-x86_64-unknown-linux-musl/cargo-deny"

FROM docker.io/golang:${GO_VERSION}-bullseye as yq
ARG YQ_VERSION=v4.25.1
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -vo /usr/local/bin/yq "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" \
    && chmod +x /usr/local/bin/yq

FROM docker.io/golang:${GO_VERSION}-bullseye as kubectl
ARG KUBECTL_VERSION=v1.24.2
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -vo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
    && chmod 755 /usr/local/bin/kubectl

FROM docker.io/golang:${GO_VERSION}-bullseye as k3d
ARG K3D_VERSION=v5.4.3
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -v https://raw.githubusercontent.com/rancher/k3d/$K3D_VERSION/install.sh \
    | USE_SUDO=false K3D_INSTALL_DIR=/usr/local/bin bash

FROM docker.io/golang:${GO_VERSION}-bullseye as just
ARG JUST_VERSION=1.1.3
RUN curl --proto '=https' --tlsv1.3 -vsSfL "https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-x86_64-unknown-linux-musl.tar.gz" \
    | tar zvxf - -C /usr/local/bin just

FROM docker.io/golang:${GO_VERSION}-bullseye as nextest
ARG NEXTEST_VERSION=0.9.14
RUN curl --proto '=https' --tlsv1.3 -vsSfL "https://github.com/nextest-rs/nextest/releases/download/cargo-nextest-${NEXTEST_VERSION}/cargo-nextest-${NEXTEST_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
    | tar zvxf - -C /usr/local/bin cargo-nextest

FROM docker.io/golang:${GO_VERSION}-bullseye as actionlint
ARG ACTION_LINT_VERSION=1.6.15
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -v "https://raw.githubusercontent.com/rhysd/actionlint/v${ACTION_LINT_VERSION}/scripts/download-actionlint.bash" \
  | bash -s -- "${ACTION_LINT_VERSION}" /usr/local/bin

FROM docker.io/rust:${RUST_TOOLCHAIN}-bullseye as protoc
ARG PROTOC_VERSION=v3.20.1
WORKDIR /tmp
RUN arch="$(uname -m)" ; \
    version="$PROTOC_VERSION" ; \
    curl --proto '=https' --tlsv1.3 -vsSfLo protoc.zip  "https://github.com/google/protobuf/releases/download/$version/protoc-${version#v}-linux-$arch.zip" && \
    unzip protoc.zip bin/protoc && \
    chmod 755 bin/protoc

FROM docker.io/rust:${RUST_TOOLCHAIN}-bullseye as rust
RUN rustup component add rustfmt clippy rls

##
## Main container configuration
##

FROM docker.io/golang:${GO_VERSION}-bullseye

ENV DEBIAN_FRONTEND=noninteractive
RUN apt update && \
    apt upgrade -y --autoremove && \
    apt install -y \
        clang \
        cmake \
        jq \
        libssl-dev \
        lldb \
        locales \
        lsb-release \
        npm \
        shellcheck \
        sudo \
        time \
        unzip && \
    rm -rf /var/lib/apt/lists/*
RUN npm install markdownlint-cli2@0.4.0 --global

RUN sed -i 's/^# *\(en_US.UTF-8\)/\1/' /etc/locale.gen && locale-gen

ARG USER=code
ARG USER_UID=1000
ARG USER_GID=1000
RUN groupadd --gid=$USER_GID $USER \
    && useradd --uid=$USER_UID --gid=$USER_GID -m $USER \
    && echo "$USER ALL=(root) NOPASSWD:ALL" >/etc/sudoers.d/$USER \
    && chmod 0440 /etc/sudoers.d/$USER

# Install a Docker client that uses the host's Docker daemon
ARG USE_MOBY=false
ENV DOCKER_BUILDKIT=1
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -v https://raw.githubusercontent.com/microsoft/vscode-dev-containers/main/script-library/docker-debian.sh \
    | bash -s --  true /var/run/docker-host.sock /var/run/docker.sock "${USER}" "${USE_MOBY}" latest

RUN (echo "LC_ALL=en_US.UTF-8" \
    && echo "LANGUAGE=en_US.UTF-8") >/etc/default/locale

USER $USER
ENV USER=$USER
ENV HOME=/home/$USER

COPY --from=go /go/bin /go/bin
COPY --from=cargo-deny /usr/local/bin/cargo-deny /usr/local/bin/cargo-deny
COPY --from=k3d /usr/local/bin/k3d /usr/local/bin/k3d
COPY --from=kubectl /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=yq /usr/local/bin/yq /usr/local/bin/yq
COPY --from=just /usr/local/bin/just /usr/local/bin/just
COPY --from=nextest /usr/local/bin/cargo-nextest /usr/local/bin/cargo-nextest
COPY --from=actionlint /usr/local/bin/actionlint /usr/local/bin/actionlint

COPY --from=protoc /tmp/bin/protoc /usr/local/bin/protoc
ENV PROTOC_NO_VENDOR=1
ENV PROTOC=/usr/local/bin/protoc

COPY --from=rust /usr/local/cargo /usr/local/cargo
COPY --from=rust /usr/local/rustup /usr/local/rustup
ENV CARGO_HOME=/usr/local/cargo
ENV RUSTUP_HOME=/usr/local/rustup
RUN sudo chmod 777 $CARGO_HOME $RUSTUP_HOME
ENV PATH=/usr/local/cargo/bin:$PATH

RUN scurl -v https://run.linkerd.io/install-edge | sh
ENV PATH=$HOME/.linkerd2/bin:$PATH

ENTRYPOINT ["/usr/local/share/docker-init.sh"]
CMD ["sleep", "infinity"]
