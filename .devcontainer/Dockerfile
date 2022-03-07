ARG GO_VERSION=1.17
ARG RUST_TOOLCHAIN=1.59.0

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
    ; do go install "$p" ; done

FROM docker.io/golang:${GO_VERSION}-bullseye as cargo-deny
ARG CARGO_DENY_VERSION=0.11.3
COPY bin/scurl /usr/local/bin/scurl
RUN scurl "https://github.com/EmbarkStudios/cargo-deny/releases/download/${CARGO_DENY_VERSION}/cargo-deny-${CARGO_DENY_VERSION}-x86_64-unknown-linux-musl.tar.gz" \
    | tar zvxf - --strip-components=1 -C /usr/local/bin "cargo-deny-${CARGO_DENY_VERSION}-x86_64-unknown-linux-musl/cargo-deny"

FROM docker.io/golang:${GO_VERSION}-bullseye as yq
ARG YQ_VERSION=v4.2.0
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -vo /usr/local/bin/yq "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" \
    && chmod +x /usr/local/bin/yq

FROM docker.io/golang:${GO_VERSION}-bullseye as kubectl
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -vo /usr/local/bin/kubectl "https://dl.k8s.io/release/$(scurl https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
    && chmod 755 /usr/local/bin/kubectl

FROM docker.io/golang:${GO_VERSION}-bullseye as k3d
ARG K3D_VERSION=v5.3.0
COPY bin/scurl /usr/local/bin/scurl
RUN scurl -v https://raw.githubusercontent.com/rancher/k3d/$K3D_VERSION/install.sh \
    | USE_SUDO=false K3D_INSTALL_DIR=/usr/local/bin bash

FROM docker.io/rust:${RUST_TOOLCHAIN}-bullseye as rust
RUN rustup component add rustfmt clippy rls

##
## Main container configuration
##

FROM docker.io/golang:${GO_VERSION}-bullseye

# Note: we do *not* delete the apt cache so subsequent steps (like docker,
# dotfiles) need not pull the cache again. This comes at the cost of a fatter
# container image.
ENV DEBIAN_FRONTEND=noninteractive
RUN apt update && apt upgrade -y
RUN apt install -y \
    clang \
    cmake \
    jq \
    libssl-dev \
    lldb \
    locales \
    lsb-release \
    npm \
    sudo \
    time
RUN npm install markdownlint-cli@0.23.1 --global

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
