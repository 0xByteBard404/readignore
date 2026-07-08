# Contributing to readignore

Thanks for your interest in contributing to **readignore**! This document
describes the development workflow and a few conventions we follow. Every
contribution is welcome and appreciated.

## Code of Conduct

By participating in this project you agree to abide by our
[Code of Conduct](./CODE_OF_CONDUCT.md). Please read it and keep it in mind in
all interactions within the community.

## How to Contribute

1. **Fork** the repository to your own GitHub account.
2. **Clone** your fork locally:
   ```bash
   git clone https://github.com/<your-username>/readignore.git
   cd readignore
   ```
3. **Add an upstream remote** so you can keep your fork in sync:
   ```bash
   git remote add upstream https://github.com/0xByteBard404/readignore.git
   git fetch upstream
   ```
4. **Create a feature branch** from the latest `main`:
   ```bash
   git checkout main
   git pull upstream main
   git checkout -b feat/my-awesome-feature
   ```
5. **Make your changes**, keeping commits focused and small.
6. **Run the local checks** (see below) and make sure they pass.
7. **Push** your branch to your fork:
   ```bash
   git push -u origin feat/my-awesome-feature
   ```
8. **Open a Pull Request** against `main` and fill in the PR description. Link
   any related issues and explain the motivation and approach.

## Commit Conventions

This project follows the [**Conventional Commits**](https://www.conventionalcommits.org/)
specification. Please prefix commit subjects with a type:

```
<type>(<optional scope>): <short description in lowercase>

<optional body>
<optional footer(s)>
```

Common types:

| Type     | Use for                                                         |
| -------- | --------------------------------------------------------------- |
| `feat`   | A new feature                                                   |
| `fix`    | A bug fix                                                       |
| `docs`   | Documentation-only changes                                      |
| `style`  | Formatting, whitespace, semicolons — no code logic             |
| `refactor` | Code change that neither fixes a bug nor adds a feature       |
| `perf`   | A code change that improves performance                         |
| `test`   | Adding or correcting tests                                      |
| `chore`  | Build, tooling, dependencies, CI, etc.                          |
| `ci`     | Changes to CI configuration files and scripts                   |
| `revert` | Reverting a previous commit                                     |

Keep the subject line short and imperative ("add parser", not "added parser").

## Local Development

We use `make` to run the common tasks. The available targets are:

```bash
make build   # build the binary into dist/
make test    # run all tests with -race -cover
make lint    # run golangci-lint
make fmt     # gofmt -s -w .
make tidy    # go mod tidy
make clean   # remove dist/
```

Before opening a PR, please make sure the following all pass locally:

```bash
make fmt
make lint
make test
make tidy
```

> **Note:** `make lint` requires
> [`golangci-lint`](https://golangci-lint.run/) to be installed on your PATH.

## Pull Request Checklist

- [ ] Branch is based on the latest `main`.
- [ ] Commits follow Conventional Commits.
- [ ] `make fmt && make lint && make test` all pass locally.
- [ ] Tests cover new behavior where reasonable.
- [ ] Public functions/types have doc comments.
- [ ] The PR description explains the *why*, not just the *what*, and links
      related issues.

## Licensing

By submitting a pull request, you confirm that:

- You own the copyright on your contribution, or you have the right to submit
  it under the project's license; **and**
- You agree that your contribution will be licensed under the project's
  [MIT License](./LICENSE), and that this licensing is final and
  non-revocable.

There is no separate Contributor License Agreement (CLA) for this project — the
above signing statement is sufficient.

## Reporting Issues and Security Bugs

- For regular bugs and feature requests, please
  [open a GitHub issue](https://github.com/0xByteBard404/readignore/issues).
- For security-sensitive vulnerabilities, **do not open a public issue**.
  Instead, follow the private reporting process described in our
  [Security Policy](./SECURITY.md).

Thanks again for contributing! 🎉
