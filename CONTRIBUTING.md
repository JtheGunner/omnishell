# Contributing

## Setup

- Go 1.23+
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
