# Linkerd2 Release

This document contains instructions for releasing Linkerd2.

## 1. Bump the proxy version

Determine the commit SHA of the `linkerd2-proxy` repo to be included in the
release. If
[proxy-version](https://github.com/linkerd/linkerd2/blob/main/.proxy-version)
is already at the desired SHA, skip to step 2.

If updating to `linkerd-proxy` HEAD, note the commit SHA at
[latest.txt](https://build.l5d.io/linkerd2-proxy/latest.txt) (Look for
`linkerd2-proxy-<linkerd2-proxy-sha>.tar.gz`).

Create a new branch in the `linkerd2` repo, `username/proxy-version-bump`.

Then run:

```bash
bin/git-commit-proxy-version <linkerd2-proxy-sha>
```

The script will update the `.proxy-version` file. Submit a PR to obtain reviews
and approval.

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

## 3. Optional: push images

To facilitate testing you might want to publish the docker images to your
private repo.

First tag the release:

```bash
git tag edge-2024.3.1
```

Do *not* push this tag just yet, to avoid triggering the actual public release.

Make sure you're logged into your Github docker registry:

```bash
echo "$GH_PAT" | docker login ghcr.io -u $GH_USERNAME --password-stdin
```

Where `$GH_USERNAME` is your Github username and `$GH_PAT` is a personal access
token with enough permissions for creating Github packages.

Then this will build the images and also push them to your personal Github
docker registry (note this implies you've already set docker buildx in your
machine, if not follow [these
instructions](https://github.com/docker/buildx#installing)):

```bash
DOCKER_REGISTRY=ghcr.io/$GH_USERNAME DOCKER_MULTIARCH=1 DOCKER_PUSH=1 bin/docker-build
```

If this is the first time you push those images into your personal registry,
you'll need to go to `https://github.com/$GH_USERNAME?tab=packages` and access
the settings for each image in order to make them public.

After having successfully pushed those images, delete the tag so you can create
it again and push it for good as explained in the following step.

Now testers can pull the CLI binaries through this:

```bash
DOCKER_REGISTRY=ghcr.io/$GH_USERNAME bin/docker-pull-binaries edge-2024.3.1
```

which will dump them under the `target/release` directory.

Besides using those particular binaries to install Linkerd, they'll also need to
point to your registry using the `--registry` flag.

```bash
target/release/linkerd2-cli-edge-2024.3.1-darwin install --registry ghcr.io/$GH_USERNAME -f ~/tmp/linkerd-overrides.yml
```

## 4. Tag the release

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

## 5. Do a walkthrough verification of the release

Go to the release page in Github and check that the notes are properly
formatted and the files are there.  Download the file for your system along
with its checksum, verify the checksum matches, and do a basic sanity check:

```bash
linkerd version
linkerd install | kubectl apply -f -
linkerd check
linkerd viz install | kubectl apply -f -
linkerd viz check
linkerd dashboard
```

## 6. Send the announcement email

Send an email to <cncf-linkerd-dev@lists.cncf.io>,
<cncf-linkerd-users@lists.cncf.io>, and <cncf-linkerd-announce@lists.cncf.io>,
announcing the release.

Subscribe to these mailing lists if you aren't on them:

- [linkerd-users](https://lists.cncf.io/g/cncf-linkerd-users/join)
- [linkerd-announce](https://lists.cncf.io/g/cncf-linkerd-announce/join)
- [linkerd-dev](https://lists.cncf.io/g/cncf-linkerd-dev/join)

Make sure to include the install instructions.

> To install the CLI for this edge release, run:
<!-- markdownlint-disable MD034 -->
>
> curl --proto '=https' --tlsv1.2 -sSfL https://run.linkerd.io/install-edge | sh
>
> And please check the [upgrade
instructions](https://linkerd.io/2.12/tasks/upgrade/) for detailed steps on how
to upgrade your cluster using either the CLI or Helm.

Aftewards, include the full release notes. Liberally apply emoji. ⭐

## 7. Send an announcement to Linkerd Slack's #announcements channel

Ensure that you send a brief summary of the release in Linkerd Slack's
[#announcement](https://linkerd.slack.com/messages/C0JV5E7BR) channel.
