# 32-bit ARM support (OMNIS-30)

## Goal

omnishell installs and `omnishell apply` works on armv6l and armv7l Linux. The
built-in `mise`, `starship` and `broot` modules use an upstream release binary
on those hosts where upstream ships one, instead of the cargo source build.

Out of scope: other modules' fallbacks, non-Linux 32-bit targets.

## Upstream availability

Checked against the currently pinned tags.

| Module   | 32-bit ARM asset                               | Variants covered  |
|----------|------------------------------------------------|-------------------|
| starship | `starship-arm-unknown-linux-musleabihf.tar.gz` | armv6 and armv7   |
| mise     | `mise-<tag>-linux-armv7.tar.gz`                | armv7 only        |
| broot    | none                                           | keeps the source build |

broot keeps its `git` fallback on 32-bit ARM. This is a documented limitation.

## Design

### 1. Release builds (`.goreleaser.yaml`)

- Add a `linux/arm` build with `goarm: [6, 7]`, next to the existing
  amd64/arm64 build. Same ldflags, `CGO_ENABLED=0`.
- Archives keep the name template `omnishell_{{.Os}}_{{.Arch}}{{.Arm}}`, which
  yields `omnishell_linux_armv6.tar.gz` and `omnishell_linux_armv7.tar.gz`.
  Existing archive names are unchanged. Checksums and completions are included
  as for the other targets.
- Homebrew does not target 32-bit ARM. The formula must stay valid for the
  existing platforms; verify how GoReleaser's `brews` treats the extra archives.

### 2. Installer (`install.sh`)

| `uname -m`              | archive arch |
|-------------------------|--------------|
| `armv6l`                | `armv6`      |
| `armv7l`, `armv8l`      | `armv7`      |
| any other `arm*`        | unsupported, as today |

`armv8l` is a 32-bit userland on a 64-bit CPU. The `test/install` harness gains
cases for each mapping.

### 3. Asset selection

- `module.Asset` gets an optional `goarm` key (`"6"` or `"7"`), validated at
  manifest load. It is only valid together with `arch = "arm"`.
- `platform.Info` gets an `ARM` field holding the GOARM variant of the running
  binary, read from the `GOARM` build setting via `debug.ReadBuildInfo`. `Env`
  makes it injectable, like `GOARCH`. It is empty off 32-bit ARM and when the
  build setting is missing.
- `Fallback.AssetFor(os, arch, goarm)` prefers an asset whose `goarm` equals the
  host variant, then an asset with no `goarm`. An asset with a `goarm` never
  matches a different or unknown variant.
- `engine/plan.go` and `pkgmgr/release.go` (`ReleaseContext`) pass the variant
  through. `ComputePlan` stays side-effect-free.
- A host with no matching asset still reaches the `git` fallback.

An empty variant (a plain `go build` that did not set GOARM) matches only assets
without `goarm`. The worst case is skipping mise's armv7 asset and building from
source.

### 4. Modules

- `starship`: one `arch = "arm"` asset, no `goarm`.
- `mise`: one `arch = "arm"`, `goarm = "7"` asset.
- `broot`: unchanged.
- SHA-256 pins come from the checksums upstream publishes for the pinned tag.
- `tools/update-fallback-tags` must keep the new assets current. Its asset
  matching and rewriting (`select.go`, `rewrite.go`) and the guards in
  `release_guard_test.go` need to cope with `goarm`.

### 5. Docs

- README: supported OS / architecture matrix, including the 32-bit ARM
  limitation for broot.
- `docs/writing-a-module.md`: document the `goarm` key.
- CHANGELOG: entry under Unreleased.

## Testing

- `AssetFor`: exact variant, variant-less fallback, no cross-variant match,
  empty host variant.
- Manifest validation of `goarm` (bad value, `goarm` without `arch = "arm"`).
- Platform detection of the variant through the injectable `Env`.
- `ComputePlan` and `InstallRelease` select the right asset per variant, and an
  unmatched host falls through to git.
- `test/install` cases for the new `uname -m` values.
- `update-fallback-tags` rewrites and guards for assets with `goarm`.
- Builtin manifest tests still enforce a pinned `ref` and valid assets for the
  new entries.

## Risks

- GoReleaser `brews` with additional `linux/arm` archives. Verify with
  `goreleaser check` and a snapshot build before relying on it.
- Upstream renames of 32-bit assets would be caught by the SHA-256 pin and the
  `fallback-tags` workflow rather than by silent drift.
