# Contributing to Linkerd2 #

:balloon: Thanks for your help improving the project!

## Getting Help ##

If you have a question about Linkerd2 or have encountered problems using it,
start by [asking a question in the forums][discourse] or join us in the
[#linkerd2 Slack channel][slack].

## Developer Certificate of Origin ##

To contribute to this project, you must agree to the Developer Certificate of
Origin (DCO) for each commit you make. The DCO is a simple statement that you,
as a contributor, have the legal right to make the contribution.

See the [DCO](DCO) file for the full text of what you must agree to.

### Option 1: commit message signoffs ###

One way to signify that you agree to the DCO for a commit is to add a line to
the git commit message:

```txt
Signed-off-by: Jane Smith <jane.smith@example.com>
```

In most cases, you can add this signoff to your commit automatically with the
`-s` flag to `git commit`. You must use your real name and a reachable email
address (sorry, no pseudonyms or anonymous contributions).

### Option 2: public statement ###

If you've already made the commits and don't want to engage in git shenanigans
to retroactively apply the signoff as above, there is another option: leave a
comment on the PR with the following statement: "I agree to the DCO for all the
commits in this PR."

Note that this option also requires that your commits are made under your real
name and a reachable email address.

If you use this approach, the DCO bot will still complain, but maintainers will
override the DCO bot at merge time.

### Option 3: very simple changes ###

Changes that are trivial (e.g. spelling corrections, adding to ADOPTERS.md,
one-word changes) do not require a DCO signoff. Maintainers should feel free to
override the DCO bot for these changes.

## Submitting a Pull Request ##

Do you have an improvement?

1. Submit an [issue][issue] describing your proposed change.
2. We will try to respond to your issue promptly.
3. Fork this repo, develop and test your code changes. See the project's
   [README](README.md) for further information about working in this repository.
4. Submit a pull request against this repo's `main` branch.
    - Include instructions on how to test your changes.
    - If you are making a change to the user interface (UI), include a
      screenshot of the UI before and after your changes.
5. Your branch may be merged once all configured checks pass, including:
    - The branch has passed tests in CI.
    - A review from appropriate maintainers (see
      [MAINTAINERS.md](MAINTAINERS.md) and [GOVERNANCE.md](GOVERNANCE.md))

## Committing ##

We prefer squash or rebase commits so that all changes from a branch are
committed to main as a single commit. All pull requests are squashed when
merged, but rebasing prior to merge gives you better control over the commit
message.

### Commit messages ###

Finalized commit messages should be in the following format:

```txt
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

```txt
bad: server disconnects should cause dst client disconnects.
good: Propagate disconnects from source to destination
```

```txt
bad: support tls servers
good: Introduce support for server-side TLS (#347)
```

#### Problem ####

Explain the context and why you're making that change.  What is the problem
you're trying to solve? In some cases there is not a problem and this can be
thought of as being the motivation for your change.

#### Solution ####

Describe the modifications you've made.

If this PR changes a behavior, it is helpful to describe the difference between
the old behavior and the new behavior. Provide before and after screenshots,
example CLI output, or changed YAML where applicable.

Describe any implementation changes which are particularly complex or
unintuitive.

List any follow-up work that will need to be done in a future PR and link to any
relevant Github issues.

#### Validation ####

Describe the testing you've done to validate your change.  Give instructions for
reviewers to replicate your tests.  Performance-related changes should include
before- and after- benchmark results.

[discourse]: https://discourse.linkerd.io/c/linkerd2
[issue]: https://github.com/linkerd/linkerd2/issues/new
[slack]: http://slack.linkerd.io/
