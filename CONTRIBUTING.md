# Contributing to Linkerd2 #

:balloon: Thanks for your help improving the project!

## Getting Help ##

If you have a question about Linkerd2 or have encountered problems using it,
start by [asking a question in the forums][discourse] or join us in the
[#linkerd2 Slack channel][slack].

## Certificate of Origin ##

By contributing to this project you agree to the Developer Certificate of
Origin (DCO). This document was created by the Linux Kernel community and is a
simple statement that you, as a contributor, have the legal right to make the
contribution. See the [DCO](DCO) file for details.

In practice, just add a line to every git commit message:

```
Signed-off-by: Jane Smith <jane.smith@example.com>
```

Use your real name (sorry, no pseudonyms or anonymous contributions).

If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.

## Submitting a Pull Request ##

Do you have an improvement?

1. Submit an [issue][issue] describing your proposed change.
2. We will try to respond to your issue promptly.
3. Fork this repo, develop and test your code changes. See the project's [README](README.md) for further information about working in this repository.
4. Submit a pull request against this repo's `master` branch.
5. Your branch may be merged once all configured checks pass, including:
    - The branch has passed tests in CI.
    - A review from appropriate maintainers (see [MAINTAINERS.md](MAINTAINERS.md) and [GOVERNANCE.md](GOVERNANCE.md))

## Committing ##

We prefer squash or rebase commits so that all changes from a branch are
committed to master as a single commit. All pull requests are squashed when
merged, but rebasing prior to merge gives you better control over the commit
message.

### Commit messages ###

Finalized commit messages should be in the following format:

```
Subject

Problem

Solution

Validation

Fixes #[GitHub issue ID]
```

#### Subject ####

- one line, <= 50 characters
- describe what is done; not the result
- use the active voice
- capitalize first word and proper nouns
- do not end in a period — this is a title/subject
- reference the GitHub issue by number

##### Examples #####

```
bad: server disconnects should cause dst client disconnects.
good: Propagate disconnects from source to destination
```

```
bad: support tls servers
good: Introduce support for server-side TLS (#347)
```

#### Problem ####

Explain the context and why you're making that change.  What is the problem
you're trying to solve? In some cases there is not a problem and this can be
thought of as being the motivation for your change.

#### Solution ####

Describe the modifications you've made.

#### Validation ####

Describe the testing you've done to validate your change.  Performance-related
changes should include before- and after- benchmark results.

[discourse]: https://discourse.linkerd.io/c/linkerd2
[issue]: https://github.com/linkerd/linkerd2/issues/new
[slack]: http://slack.linkerd.io/
