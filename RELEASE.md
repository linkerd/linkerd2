# Linkerd2 Release

This document contains instructions for releasing Linkerd2.

## 1. Bump the proxy version

Determine the commit SHA or tag of the `linkerd2-proxy` repo to be included in
the release.

The [proxy-version](https://github.com/linkerd/linkerd2/blob/main/.proxy-version)
file is kept in sync automatically by the
[`sync-proxy`](https://github.com/linkerd/linkerd2/actions/workflows/sync-proxy.yml)
workflow. If the file is already at the desired SHA or tag, skip to step 2.

If updating to `linkerd-proxy` HEAD, note the commit SHA at
[latest.txt](https://build.l5d.io/linkerd2-proxy/latest.txt) (Look for
`linkerd2-proxy-<linkerd2-proxy-sha>.tar.gz`).

## 2. Bump the proxy-init or CNI plugin version

If the `linkerd2/proxy-init` or `linkerd2/cni-plugin` projects have a new
release (which is rare), the following updates are needed:

- `pkg/version/version.go` (this also implies changes in unit test fixtures)

   ```go
   var ProxyInitVersion = "v2.3.0"
   var LinkerdCNIVersion = "v1.4.0"
   ```

- `charts/linkerd-control-plane/values.yaml`

   Upgrade the version in `global.proxyInit.image.version`

- `charts/linkerd2-cni/values.yaml`

   Upgrade the version in `image.version`

Create a new branch in the `linkerd2` repo,
`username/proxy-init-version-bump`.

Open a pull request that includes the changes.

## 3. Tag the release

- Checkout the `main` branch
- Tag, e.g. `edge-24.3.1`
- Push the tag

That will kick off a CI Release workflow run that will:

- Build and push the docker images for the tag that was created
- Run the k3d integration tests in the Github actions VMs themselves
- Run a k3d integration test on a separate ARM64 host
- Create a release in Github, and upload the CLI binaries with their checksums
- Dispatch an event caught by the website repo that triggers a website rebuild
  which will update the edge version in the website
- Retrieve the installation script from [run.linkerd.io](https://run.linkerd.io)
  and verify it installs the current version being released
- Deploy the updated helm charts

You can locate the CI run on the [actions page](https://github.com/linkerd/linkerd2/actions).
