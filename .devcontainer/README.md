# devcontainer

This directory provides a _devcontainer_ configuration that configures a
reproducible development environment for this project.

The devcontainer configuration is maintained in the
[linkerd/dev](https://github.com/linkerd/dev) repository.

## Docker

This configuration currently uses the parent host's Docker daemon (rather than
running a separate docker daemon within in the container). It creates
devcontainers on the host network so it's easy to use k3d clusters hosted in the
parent host's docker daemon.

## Customizing

This configuration is supposed to provide a minimal setup without catering to
any one developer's personal tastes. Devcontainers can be extended with per-user
configuration.

To add your own extensions to the devcontainer, configure default extensions in
your VS Code settings:

```jsonc
    "remote.containers.defaultExtensions": [
        "eamodio.gitlens",
        "GitHub.copilot",
        "GitHub.vscode-pull-request-github",
        "mutantdino.resourcemonitor",
        "stateful.edge"
    ],
```

Furthermore, you can configure a _dotfiles_ repository to perform customizations
with a configuration like:

```jsonc
    "dotfiles.repository": "https://github.com/olix0r/dotfiles.git",
```
