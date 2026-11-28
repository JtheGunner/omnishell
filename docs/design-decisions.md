# Design decisions and deferred ideas

Rationale that is not obvious from the code, and ideas that were considered but
not built. For how the features work, see the [README](../README.md),
[CONTRIBUTING.md](../CONTRIBUTING.md) and [writing-a-module.md](writing-a-module.md).

## Release-binary fallback

- **No distro-specific apt repository for `mise`.** A pinned release binary
  works on apt, dnf, zypper and apk without sudo or key management. A
  per-distro repository would sit outside the `Manager` model.
- **The SHA-256 pins an artifact, it does not authenticate upstream.** It
  guarantees that the binary is the one the maintainer reviewed. Signature
  schemes (SLSA, minisign) are deliberately not verified.
- **The checksum comes from the GitHub release `digest` field**, because it is
  the only checksum source all supported upstreams provide.

## Rollback

- **Rollback overwrites hand-edited files on purpose.** There is no hand-edit
  guard, because restoring a known state is the point.
- **`config.toml` is not part of the snapshot.** After a rollback it may
  disagree with the restored state; `doctor` reports that as ordinary drift.
- **A rollback takes its own pre-rollback snapshot**, so it can be undone.
- **`uninstall --purge` snapshots live in a temp directory** and are not
  covered by `rollback`.

## Doctor

- Unknown and malformed modules are reported at `notice` severity so they do not
  cause exit code 3, consistent with the `module-override` notice.

## Non-shell files

- **omnishell writes only its init files and the rc marker block.** Modules
  must not create or edit other files such as `~/.gitconfig`.
- **Why:** backup, rollback, `doctor` and drift detection are built around the
  init files. Extending them to arbitrary third-party config files would mean
  merging with hand-written content and owning its format, which is a larger
  and riskier surface than the benefit.
- **Consequence for `delta`:** the module sets `GIT_PAGER` from the shell
  environment. It is only exported when `delta` is on `PATH` and `GIT_PAGER` is
  unset, so existing user settings win. `GIT_PAGER` overrides `core.pager`
  without editing it. `interactive.diffFilter` (for `git add -p`) cannot be
  set through the environment and is left to the user.

## Self-update

`omnishell self-update` (`internal/selfupdate`) mirrors `install.sh`:

- **Latest version:** the tag comes from the `Location` header of
  `github.com/JtheGunner/omnishell/releases/latest`, which is never followed
  and must stay on the requested host. No REST API, so no rate limit.
- **Verification:** the archive `omnishell_<os>_<arch>.tar.gz` (armv6/armv7 on
  Linux) and `checksums.txt` come from the release download URL over HTTPS
  (the shared `pkgmgr.Downloader`). The SHA-256 must match before anything is
  extracted; only the `omnishell` member is read, with a size limit, and an
  archive with absolute or `..` entries is rejected.
- **Replacement:** the binary is written to a temp file in the running binary's
  own directory, given the old file's mode and renamed over it. Any error
  removes the temp file and leaves the old binary untouched. That temp file is
  also the writability probe, so a read-only directory fails before any
  download. There is no sudo and no escalation.
- **Homebrew:** a symlink-resolved path with a `Cellar` segment, or under
  `/opt/homebrew/`, `/usr/local/Cellar/` or `/home/linuxbrew/.linuxbrew/`, is
  not replaced; the command prints `brew upgrade jthegunner/tap/omnishell`
  and exits 0.
- **Versions:** only a strictly newer semver tag updates. A binary that is
  ahead of the latest release counts as up to date. A development build
  (`dev` or no semver) never replaces itself, but `--check` still prints the
  latest tag.
- **Not covered:** background updates and module updates.

## Deferred ideas

Not planned, not tracked in the issue tracker yet.

| Idea | Sketch |
|:--|:--|
| **fish support** | Touches `rcfile`, `initfile` and `platform/shells.go`; needs a `fish.tmpl` per module; `~/.config/fish/config.fish` uses different marker and comment syntax. |
| **Profiles** | A `--profile` flag plus a namespaced `config.toml` section; touches `config` and `ComputePlan`. |
| **Presets / bundles** | Named groups of modules enabled together; a larger change to the config model. |
| **Remote module registry** | Reuse the checksum and signature pattern of `install.sh` for authenticity. |
| **`keychain` (ssh-agent) module** | Not selected so far. |
