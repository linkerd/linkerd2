# Agent Instructions

## Scope

All files in this repository.

## Dev Tips

When making changes to the code, you may run a subset of validations related to
the changes being made:

### Go

- `just go-fetch` fetches dependencies
- `just go-fmt` formats
code
- `just go-lint` lints code
- `just go-test` runs unit tests

### Rust

- `just rs-fetch` fetches dependencies
- `just rs-fmt` formats code
- `just rs-clippy` lints code
- `just rs-test` runs unit tests

### Shell

- `just sh-lint` runs shellcheck on scripts

### Markdown

- `just md-lint` runs markdownlint on documentation

## Commit Messages

- Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification.
- Use a short summary (<=50 characters) and include a scope when appropriate,
  e.g. `feat(cli): add --super-duper mode flag`.
- Each commit must contain a `Signed-off-by: Your Name <email>` line to satisfy
  the DCO.

### Pull Requests

- Describe the problem being solved, the solution, and how to validate it.
- Include the commands used for testing so reviewers can reproduce your
  results.
