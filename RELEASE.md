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

## 2. Bump the proxy-init version

If the `linkerd2/proxy-init` project has a new release (which is rare), the
following updates are needed:

- `go.mod`

   ```mod
   github.com/linkerd/linkerd2-proxy-init v1.2.0
   ```

- `pkg/version/version.go` (this also implies changes in unit test fixtures)

   ```go
   var ProxyInitVersion = "v1.2.0"
   ```

- `charts/linkerd2/values.yaml`

   Upgrade the version in `global.proxyInit.image.version`

Create a new branch in the `linkerd2` repo,
`username/proxy-init-version-bump`.

Open a pull request that includes the changes.

## 3. Create a minor releases branch

**This step only applies to minor stable releases (e.g. `2.9.1`).**

If it doesn't exist yet, create and push a branch in the `linkerd2` repo where
all the minor releases for a given major release will reside:

```bash
git checkout -b release/2.9
git push -u origin release/2.9
```

The branch in the following step should be based off of this one.

## 4. Create the release branch

Create a branch in the `linkerd2` repo, `username/edge-X.X.X` (replace with
your name and the actual release number, optionally replace `edge` with
`stable`).

## 5. Cherry pick changes from `main`

**This step only applies to minor stable releases (e.g. `2.9.1`).**

Locate all the commits in the git log that happened since the last stable
release, that you'd like to include in the current minor stable release, and
cherry-pick each one into the current branch using their corresponding SHAs:

```bash
git cherry-pick ae34bcc2
git cherry-pick b34effab
git cherry-pick 223bd232
...
```

Each step might result in conflicts that you'll need to address.

## 6. Update the Helm charts versions

All the Helm charts (linkerd-crds, linkerd-control-plane, linkerd2-cni,
linkerd-multicluster, linkerd-jaeger and linkerd-viz) have a `version` entry
with a semver format `major.minor.patch[-edge]` that needs to be updated
according to the rules below.

Note that the `appVersion` entry (for those charts that have it) is handled
automatically by CI.

Also keep in mind chart version changes require updating the charts README
files (through `bin/helm-docs`) and golden files (through `go test ./...
-update`).

Example:

```text
Stable release:
1.9.0

Subsequent edges:
1.10.0-edge
1.10.1-edge

Stable maintenance release:
1.9.1

Subsequent edges:
1.10.2-edge
1.10.3-edge

Stable release:
1.11.0

Subsequent edges:
1.12.0-edge
1.12.1-edge
```

### Edge releases

When making the first edge release right after a stable one, bump the minor and
reset the patch. This leaves room for eventual maintenance stable release
versions.

MOST COMMON CASE: If making an edge release after another edge release, just
bump the patch.

In any case remember to keep the `-edge` suffix.

### Stable releases

When making a new stable release off of `main` (new major `2.x.0` release):

- reset patch
- If there are breaking changes, most notably changes to the structure of
  `values.yaml`: bump major and reset minor
- If the changes are non-breaking: bump minor (with respect to previous edge)
- remove the `-edge` suffix

When making a stable maintenance release off of a `release/stable-2.x` branch
(new `2.x.y` release), just bump the minor.

In any case remember to remove the `-edge` suffix.

### linkerd-crds

Almost all the charts are always updated, at least because the docker image
versions referred in their templates change with each release. One exception is
`linkerd-crds`, which doesn't contain image references. So this chart only
requires bumping its `version` if there were changes in its templates files.

## 7. Update the release notes

On this branch, add the release notes for this version in `CHANGES.md`.

Note: To see all of the changes since the previous release, run the command
below in the `linkerd2` repo. If the last release was a stable release, be
sure to use `stable-Y.Y.Y` instead of `edge-Y.Y.Y`.

```bash
git log edge-Y.Y.Y..HEAD
```

And this command in the `linkerd2-proxy` repo:

```bash
git log release/vX.X.X..release/vY.Y.Y
```

Where `release/vX.X.X` is the version of the proxy from the last release
and `release/vY.Y.Y` is the version of the proxy for this release, e.g.:

```bash
git log release/v2.102.0..release/v2.103.0
```

## 8. Post a PR that includes the changes

If you're preparing a minor release, make sure the PR's merge target is the
releases branch you created above (e.g. `releases/stable-2.9`). For the other
cases the target should just be `main`.

This PR needs an approval from a "code owner." Feel free to ping one of the
code owners if you've gotten feedback and approvals from other team members.

## 9. Optional: push images

To facilitate testing (particularly for stable releases) you might want to
publish the docker images in your private repo.

First tag the release:

```bash
git tag stable-2.9.1
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
DOCKER_REGISTRY=ghcr.io/$GH_USERNAME bin/docker-pull-binaries stable-2.9.1
```

which will dump them under the `target/release` directory.

Besides using those particular binaries to install Linkerd, they'll also need to
point to your registry using the `--registry` flag. Currently that flag doesn't
apply to add-ons, so you need to also recur to the `--config` flag. Currently
that only applies to Grafana:

```bash
$ cat linkerd-overrides.yml
grafana:
  enabled: true
  image:
    name: ghcr.io/alpeb/grafana

$ target/release/linkerd2-cli-stable-2.9.1-darwin install --registry ghcr.io/$GH_USERNAME --config ~/tmp/linkerd-overrides.yml
```

## 10. Merge release notes branch, then create the release tag

After the review has passed and the branches from step 2 and 4 have been merged,
follow the instructions below to properly create and push the release tag from
the appropriate branch.

**Note**: The release script will create a GPG-signed tag, so users must have
GPG signing setup in their local git config.

If performing an edge release then issue these commands. The appropriate tag
will be automatically calculated:

```bash
git checkout main
git pull
./bin/create-release-tag edge
```

If performing a stable release then issue these commands instead (in this case
you need to explicitly pass the version to be released):

```bash
git checkout main
git pull
./bin/create-release-tag stable x.x.x
```

In both cases follow the instructions on screen for pushing the tag upstream.

That will kick off a CI Release workflow run that will:

- Build and push the docker images for the tag that was created
- Run the k3d integration tests in the Github actions VMs themselves
- Run a k3d integration test on a separate ARM64 host
- Create a release in Github, and upload the CLI binaries with their checksums
- Dispatch an event caught by the website repo that triggers a website rebuild
  which will update the edge/stable versions in the website
- Retrieve the installation script from [run.linkerd.io](https://run.linkerd.io)
  and verify it installs the current version being released
- Deploy the updated helm charts

You can locate the CI run [here](https://github.com/linkerd/linkerd2/actions).

## 11. Do a walkthrough verification of the release

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

- Install linkerd onto your cluster, and run a few CLI commands
- Open the dashboard and click around
- Click through to Grafana
- When finished with this step, you may want to delete your `alias` to prevent
  confusion later

## 12. Send the announcement email

Send an email to cncf-linkerd-dev@lists.cncf.io,
cncf-linkerd-users@lists.cncf.io, and cncf-linkerd-announce@lists.cncf.io,
announcing the release.

Subscribe to these mailing lists if you aren't on them:

- [linkerd-users](https://lists.cncf.io/g/cncf-linkerd-users/join)
- [linkerd-announce](https://lists.cncf.io/g/cncf-linkerd-announce/join)
- [linkerd-dev](https://lists.cncf.io/g/cncf-linkerd-dev/join)

Include the full release notes in the email. Liberally apply emoji. ⭐

## 13. Send an announcement to Linkerd Slack's #announcements channel

Ensure that you send a brief summary of the release in Linkerd Slack's
[#announcement](https://linkerd.slack.com/messages/C0JV5E7BR) channel.

## 14. Add a community page announcement to the website repo

When doing a `stable-X.X.X` be sure to also include an announcement page for
the Linkerd2 dashboard "Community" sidebar button.

In the [website](https://github.com/linkerd/website) repo:

1. Run `hugo new --contentDir linkerd.io/content/dashboard/ YYYYMMDD.md`
2. Open the newly created file in your favorite editor and change the `title`
   to match the announcement email title.
3. Remove the `draft: true` section in the file and then add a brief summary
   of the stable release.
4. cd to the directory `linkerd.io`.
5. Run `hugo serve` in the directory to test your changes.
6. Verify your change by navigating to `http://localhost:1313/dashboard`. Make
   sure that the announcement appears at the top of the page.
7. Once you are satisfied with your changes, Post the PR for review.
   Once merged, the change should deploy automatically.
