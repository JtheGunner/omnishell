# Contributing

## Setup

- Go 1.26+
- `go build ./...` should work with no extra setup — no external services,
  no network access required for tests.

## Commands

```sh
make build   # go build with version ldflags -> ./omnishell
make test    # go test ./... -race -count=1
make vet     # go vet ./...
make lint    # golangci-lint run (CI uses golangci-lint-action v9, config v2.13)
```

Run a single test: `go test ./internal/engine/... -run TestApply_Foo -v`

All four (`build`, `test`, `vet`, `lint`) must pass before opening a PR — CI
runs the same checks (`.github/workflows/ci.yml`) on every push/PR, plus a
build-matrix job on ubuntu+macos.

## Tests

Every package that touches the filesystem, package managers, or shell
execution takes injected fakes (see `internal/pkgmgr/mock.go`) — new code
should follow the same pattern rather than shelling out or touching the real
filesystem directly, so it stays testable without root or a real `$HOME`.

`test/e2e/run.sh` builds the binary and runs a real
init→enable→apply→doctor→remove→uninstall cycle. It's meant to run as root in
a minimal distro container (CI does this per-OS) — don't run it against your
own `$HOME`.

## Adding or changing a built-in module

Built-in modules live in `modules/builtin/<id>/` and are embedded into the
binary at build time (`modules/embed.go`). The manifest schema, template
context, and a full worked example are in
[docs/writing-a-module.md](docs/writing-a-module.md) — the same format also
works for user modules dropped into `~/.config/omnishell/modules/<id>/` with
no rebuild, so it's a good place to prototype a new built-in before vendoring
it.

## Pull requests

- Keep commits scoped; conventional commit prefixes (`feat`, `fix`, `docs`,
  `refactor`, `test`, `chore`, `perf`, `ci`) match the existing history.
- Update `CHANGELOG.md` under an "Unreleased" heading for any user-visible
  change.
- See [CLAUDE.md](CLAUDE.md) for the package-by-package architecture overview
  and the invariants that engine/apply changes must preserve.

## Pinned fallback tags

Every built-in module that falls back to a `git` clone pins a release tag in its manifest (`ref`). The `fallback-tags` workflow (`.github/workflows/fallback-tags.yml`) looks for newer stable tags every Monday and opens a pull request that bumps them. Run it by hand from the Actions tab (`workflow_dispatch`) or locally:

```sh
go run ./tools/update-fallback-tags --summary summary.md
```

Before merging such a pull request, check that each module's `requires` still matches the toolchain the new tag needs; CI does not build the fallbacks.

Two optional repository settings:

- **`FALLBACK_TAGS_TOKEN`** (secret): a fine-grained personal access token limited to this repository (contents and pull requests: read and write). Without it the pull request is created with the default token and `ci` does not run on it.
- **Auto-merge:** set the repository variable `FALLBACK_TAGS_AUTOMERGE` to `true` to let the workflow request auto-merge for pull requests whose bumps keep their major version. It only takes effect when *Allow auto-merge* is enabled and `main` has branch protection with required status checks; otherwise the workflow leaves the pull request open and says so in the job summary.

The workflow also needs *Settings → Actions → General → Allow GitHub Actions to create and approve pull requests*.
