# Release-binary fallback

Issue: OMNIS-28 (follows OMNIS-25, which deliberately left release binaries out; builds on OMNIS-26/27, which pinned and now maintain the `git` fallback refs).

## Problem

On a system without a package for a module, omnishell builds it from source: `git clone` + `cargo install`. For `mise` on Ubuntu 24.04 that takes about 22 minutes, needs a current Rust toolchain plus `cmake`, and prints Cargo warnings that come from upstream's own lockfile and cannot be silenced with `--locked`. `mise`, `starship` and `broot` all publish ready-made Linux release binaries.

## Goals

- A new fallback type `release` that downloads a pinned, SHA-256-verified binary into `{{.VendorDir}}/bin`.
- Fallback order: system package → release binary → Cargo build. The Cargo build stays as the last resort.
- The built-in `mise`, `starship` and `broot` manifests use it.
- Re-running is idempotent; nothing is downloaded or built when the pinned version is already present.
- Moving a machine from a Cargo build to a release binary removes the old build's leftovers, visibly in the plan, without deleting anything that is not provably ours.
- Download and checksum failures produce a clear message and mark the module degraded.

## Non-goals

- Supporting `mise`'s own apt repository. It needs sudo and key management, only covers apt, and sits outside the `Manager` model. The release binary already covers apt, dnf, zypper and apk.
- macOS release assets. macOS uses brew; only Linux `amd64` and `arm64` are in scope.
- glibc-specific assets or libc detection (see "Asset selection").
- Verifying release signatures (SLSA, minisign). The checksum pins the artifact the maintainer reviewed; it does not authenticate upstream.
- Release fallbacks for the other eight built-in modules, and any change to the package-manager paths.
- Rust/build prerequisites in the dotfiles bootstrap. They become unnecessary for these three modules; checking that is a follow-up in that repository.

## Delivery

One PR under OMNIS-28, two phases, as in OMNIS-27:

1. **Phase 1** — schema, asset selection, download/verify/install, and the three manifests. A machine on a git fallback is untouched until phase 2 handles it.
2. **Phase 2** — lockfile kind, migration cleanup, the PATH check and the tag-updater extension.

## Verified upstream facts

Checked against the GitHub Releases of the currently pinned tags (`mise` v2026.10.3, `starship` v1.26.0, `broot` v1.61.0):

| Module | Asset | Format | Member |
|---|---|---|---|
| mise | `mise-<ref>-linux-x64-musl.tar.gz`, `…-linux-arm64-musl.tar.gz` | tar.gz (~47–53 MB) | `mise/bin/mise` |
| starship | `starship-x86_64-unknown-linux-musl.tar.gz`, `starship-aarch64-unknown-linux-musl.tar.gz` | tar.gz (~5 MB) | `starship` |
| broot | `broot_<version>.zip` (one archive for all platforms, ~68 MB) | zip | `x86_64-unknown-linux-musl/broot`, `aarch64-unknown-linux-musl/broot` |

- All three publish **static musl builds for amd64 and arm64**. A static binary runs on glibc and musl systems alike.
- GitHub reports a `sha256:` digest for every release asset. `starship` additionally ships `.sha256` files; `broot` and `mise` do not. The digest is therefore the one checksum source available for all three.
- `mise` also offers raw (non-archived) binaries, but they are about three times larger than the tarballs.

## Manifest schema

A `release` entry is another `[[packages.fallback]]` table, listed before the `git` entry. `ref` keeps its meaning (the pinned upstream tag) so the lockfile and the tag updater treat both types alike.

```toml
[[packages.fallback]]
type = "release"
repo = "https://github.com/jdx/mise"   # used by the tag updater, not by apply
ref  = "v2026.10.3"
bin  = "mise"                      # installed as {{.VendorDir}}/bin/mise

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"                   # amd64 | arm64
url    = "https://github.com/jdx/mise/releases/download/{{.Ref}}/mise-{{.Ref}}-linux-x64-musl.tar.gz"
sha256 = "23c45c76567f8f1e4cfc40efff3f19e3352d3745e6c25e74891102765d1cdc3c"
member = "mise/bin/mise"           # path inside the archive; omitted = the download is the binary

[[packages.fallback.assets]]
os     = "linux"
arch   = "arm64"
url    = "https://github.com/jdx/mise/releases/download/{{.Ref}}/mise-{{.Ref}}-linux-arm64-musl.tar.gz"
sha256 = "0af3379b7a8060b047416576a746078daee3c9d2bf57fa8f6ba07d3cdf5f47aa"
member = "mise/bin/mise"

[[packages.fallback]]              # unchanged
type = "git"
…
```

- `url` is rendered with `{{.Ref}}` (the pinned tag) and `{{.Version}}` (the tag without a leading `v`, for `broot_1.61.0.zip`). It must be `https://`.
- The archive format follows the URL suffix: `.tar.gz` and `.zip` are extracted, anything else is taken as the raw binary and must not set `member`.
- **Validation** at manifest load: a `release` entry needs `repo`, `ref`, `bin` (a plain file name) and at least one asset; each asset needs `os` (`linux`), `arch` (`amd64`|`arm64`), an `https` `url` and a 64-hex `sha256`. No two assets share `(os, arch)`. `modules/builtin_test.go` additionally requires every built-in `release` entry to cover `linux/amd64` and `linux/arm64`.
- `requires` on a `release` entry is rejected; build prerequisites belong to the `git` entry only.

## Asset selection

`platform.Info` gains `Arch` (`amd64`|`arm64`, from `runtime.GOARCH`; anything else is empty). `ComputePlan` walks a module's fallbacks in order and takes the first usable entry:

- a `release` entry is usable when one asset matches `(Platform.OS, Platform.Arch)`;
- a `git` entry is always usable.

Because every asset is a static musl build, libc is not detected and the manifest has no `libc` key. An unsupported architecture (`armv7`, `riscv64`, `i386`) matches no asset, so selection reaches the `git` entry: the "unsupported combination falls back to Cargo" case. Selection is pure and needs no probe.

## Install (apply)

`Apply` handles a planned release package in the slot where it installs git fallbacks, after the hand-edit guard.

1. Download through a new injectable `Downloader` interface (next to `Runner`; tests use a fake and never touch the network). The production implementation is `net/http` with a timeout, `https` only, and a size cap, writing to a temp file under `{{.VendorDir}}` while hashing it.
2. Compare the SHA-256 with the manifest's value. A mismatch aborts before anything is extracted.
3. Extract the single `member` into a temp file. The extractor reads only the named entry and never derives output paths from archive entries, so path traversal does not apply.
4. `chmod 0755`, then `rename` onto `{{.VendorDir}}/bin/<bin>` through the existing atomic-write helper. A failure at any step leaves the previous binary in place.
5. Record `{{.VendorDir}}/bin/<bin>` in the lockfile's `VendorPaths`, so `omnishell remove` deletes it.

`--no-packages` skips all of this, as for the other fallbacks.

## Failure behavior

A download error, a checksum mismatch, or an extraction error produces a message that names the module, platform and cause, for example `mise: checksum mismatch for linux/amd64 (expected 23c4…, got 9f0a…)`. The module is marked degraded and the CLI exits non-zero, exactly as for a failed git fallback today. Apply does **not** fall through to the Cargo build at runtime: a flaky network should not become a 22-minute build, and a wrong checksum is a security signal that should not be hidden behind one. The next run retries the download. Only "no matching asset", decided at plan time, selects the Cargo build.

## State and idempotency

`lockfile.ModuleState` gains two optional fields, next to `fallback_ref`:

- `fallback_kind`: `git` or `release`. A legacy entry that has `fallback_ref` but no kind counts as `git`.
- `fallback_sha256`: the checksum of the installed release asset; empty when the binary was adopted from a Cargo build (below).

`ComputePlan` decides from the lockfile plus a pure stat of `{{.VendorDir}}/bin/<bin>`; it never executes the binary.

| Recorded state | Planned action |
|---|---|
| `release`, same `ref`, binary present | nothing |
| `release`, binary missing | download and install |
| `release`, different `ref` | download and replace the binary |
| `git`, `fallback_ref` equals `ref`, binary present | **adopt**: keep the Cargo binary, clean the build leftovers, record `release` with an empty sha256 |
| `git`, different or unknown `fallback_ref` | download and replace the binary, then clean the build leftovers |
| nothing recorded | download and install |

Adopting keeps the binary because the lockfile already says it was built from the pinned tag, so downloading it again would be duplicate work. Recording `release` afterwards matters: with the source tree gone, a `git` record would make the next run see an empty destination and re-clone.

## Cleanup of old Cargo builds

`ModulePlan` carries an explicit list of cleanup actions so that `ComputePlan` stays side-effect-free and both `apply` and `doctor` can print it. Each action is executed only if filesystem evidence shows the target is ours:

- **Source tree** `{{.VendorDir}}/<module dest>` (including `target/`): removed only when it is a git clone whose `origin` matches the manifest's `repo`. A directory that is not a clone, or whose remote differs, is left alone and logged. This reuses the check behind `ErrNotAClone`.
- **Cargo metadata:** `{{.VendorDir}}/.crates.toml` and `.crates2.json` are shared by every Cargo-installed tool under `VendorDir`. They are never deleted. Only the entry for this crate is removed, identified by the key prefix `<crate> ` together with a `path+file://<dest>` source that equals the removed source tree. If a file cannot be parsed, the entry is left in place and the log says so.
- The old Cargo binary at `{{.VendorDir}}/bin/<bin>` needs no separate action: the install replaces it, or the adopt case keeps it on purpose.

Every action is logged when executed (`removing build leftover <path>`), and a cleanup failure never degrades the module.

## PATH conflict check

The old and the new binary share one path, so a download simply replaces the Cargo build. What can still compete is another copy earlier in `PATH`, such as `/usr/local/bin/mise` or a distribution package. After a release install (and in `doctor`), a `LookPath` of `<bin>` that resolves outside `{{.VendorDir}}/bin` yields a notice naming both paths. It is a notice only: no drift, no exit-code change.

## Tag updater extension (phase 2)

`tools/update-fallback-tags` currently bumps the `ref` of `Packages.Fallback[0]`. It is extended so that for a module with a `release` entry it bumps `ref` and every asset's `sha256` together (asset URLs are templated on `{{.Ref}}`, so they follow the ref on their own):

- The new checksums come from the GitHub release API (`digest` per asset), looked up by the rendered asset's file name, through an injectable client so tests need no network.
- If any asset of a module has no digest, the module is left unchanged and the PR summary says why. A half-bumped manifest never results.
- A module's `git` entry is bumped to the same new tag when it currently pins the same tag as the `release` entry, so the Cargo fallback and the release fallback stay on one version.
- The PR summary lists the checksum changes next to the ref bump, so the reviewer sees what was pinned. This keeps the existing trust boundary: a human reviews the PR before it merges.

## Testing

Fake `Downloader`, `MockRunner`, temp directories, `-race`, in the style of the existing fallback tests:

- Download succeeds: binary at `VendorDir/bin`, mode 0755, temp file gone, path in `VendorPaths`.
- Wrong checksum: nothing installed, module degraded, message shows expected and actual hash.
- Download error and extraction error: previous binary intact, module degraded.
- Unsupported architecture: `ComputePlan` selects the `git` entry.
- Selection order: release before git; git only when no asset matches.
- Idempotency: a second apply with an unchanged lockfile plans nothing and issues no download.
- Adopt: a `git` record at the pinned ref keeps the binary, cleans leftovers, and the next run plans nothing.
- Migration with an older or unknown ref: download, replace, clean.
- Cleanup safety: a foreign directory at the dest, a clone with a different remote, a shared `.crates.toml` with other tools' entries, and an unparsable metadata file are all left untouched.
- `ComputePlan` lists the cleanup and changes nothing on disk.
- `remove` deletes the recorded release binary.
- PATH notice appears when another copy precedes `VendorDir/bin`.
- Manifest validation: missing `sha256`, bad hex, unknown `arch`, duplicate `(os, arch)`, `http` URL, `member` on a raw asset, `requires` on a release entry. Every built-in `release` entry covers both architectures.
- Updater: ref, url and sha256 rewritten together; missing digest leaves the module unchanged and is reported.
- Lockfile: the new fields round-trip, and a lockfile without them reads as legacy `git`.

## Documentation

- `README.md`: build prerequisites per module, with `requires` now applying only to the Cargo fallback.
- `docs/writing-a-module.md`: the `release` type, the asset schema and the template variables.
- `CONTRIBUTING.md`: how checksums are kept current by the updater.
- `CHANGELOG.md`: an entry for the release.
- `CLAUDE.md`: the fallback paragraph gains the release type, selection order and lockfile kind.
