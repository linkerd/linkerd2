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

If either of the [`linkerd2/proxy-init`](https://github.com/linkerd/linkerd2-proxy-init/tree/main/proxy-init) or [`linkerd2/cni-plugin`](https://github.com/linkerd/linkerd2-proxy-init/tree/main/cni-plugin) components have a new
release (which is rare), the following updates are needed:

- `pkg/version/version.go` (this also implies changes in unit test fixtures)

   ```go
   var LinkerdCNIVersion = "v1.4.0"
   ```

- `Dockerfile.proxy`

   Upgrade the proxy-init git ref in the `PROXY_INIT_REF` build arg

- `charts/linkerd2-cni/values.yaml`

   Upgrade the version in `image.version`

- `cli/cmd/testdata/*.golden` and `pkg/healthcheck/healthcheck_test.go`

   Upgrade the `cr.l5d.io/linkerd/cni-plugin` image tag in the test fixtures
   that reference it

Commit
[e36e9e1](https://github.com/linkerd/linkerd2/commit/e36e9e1bcf7248110d8d2e938aff3de0f49e6725)
is a good reference for what the change set should look like.

Create a new branch in the `linkerd2` repo,
`username/proxy-init-version-bump`.

Open a pull request that includes the changes.

## 3. Tag the release

- Checkout the `main` branch, and be sure to pull the latest changes.
- Tag the current state of `main`, with a tag of the form `edge-YY.M.N`, where
  `YY` is the year, `M` is the month, and `N` is the nth release of the month.
- Push the tag to the repository.

```sh
TAG="edge-YY.M.N"
git switch main
git pull --tags --prune
git tag $TAG HEAD
git push --tags
```

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
