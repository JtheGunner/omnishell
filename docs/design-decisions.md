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

## Remote module registry (proposal, not implemented)

Modules come from the binary (built-in) and `~/.config/omnishell/modules/`.
A remote registry would add a third source. A module's templates are shell code
that runs in every new shell, so the registry is a code-distribution channel:
authenticity and least privilege matter more than convenience. This section
records the proposed design; nothing here is built yet.

- **Hosting:** no registry is operated by this project. A registry is any
  HTTPS location that serves an `index.toml` plus one `<id>-<version>.tar.gz`
  per module. The user adds it explicitly (`[registries]` in `config.toml`);
  there is no default registry.
- **Authenticity:** a registry publishes `index.toml` signed with an Ed25519
  key (detached `index.toml.sig`). The user pins the public key in
  `config.toml` next to the registry URL (trust on explicit configuration,
  never trust on first use). The index lists each archive's SHA-256. The
  archive is verified against the index entry before anything is extracted, and
  extraction follows the same rules as `self-update` (single known layout, size
  limit, no absolute or `..` paths). A failed signature or checksum stops the
  install and leaves the module directory untouched. Ed25519 is in the Go
  standard library, so no new dependency is needed; `minisign` or `cosign` would
  add one for little gain.
- **Key management:** owned by whoever runs a registry. Rotation is a config
  edit (add the new key, remove the old one). omnishell never fetches keys from
  the network.
- **Storage and precedence:** an installed registry module is copied to
  `~/.config/omnishell/modules/<id>/` together with a `.registry.toml` record
  (registry URL, version, archive SHA-256). It is then a normal user module, so
  `rollback`, `doctor` and hand-edit detection need no new code path. A module
  whose id equals a built-in is refused unless the user passes `--override`
  explicitly; the existing override rule and its `doctor` warning then apply.
- **Privileges:** a registry module gets exactly what a user module has today,
  which is shell snippets, packages and hooks, all shown in the plan. Because a
  remote author can ship arbitrary code, `install` prints the module's packages,
  hooks and fallbacks and asks for confirmation. Hooks and git/release
  fallbacks from a registry module are disabled unless the user passes
  `--allow-hooks` at install time; the choice is recorded in `.registry.toml`.
- **Offline behaviour:** only `registry install` and `registry update` touch the
  network. `apply`, `doctor` and every other command read the local copy, so
  built-in and local modules behave as before without a network.
- **Updates:** explicit only (`registry update <id>`); a changed archive hash
  or version is shown before it replaces the local copy.
- **Out of scope:** operating a public registry, ratings, search.

Suggested build order: signature and checksum verification as a standalone
package, then `registry install` writing a user module, then update and the
`--allow-hooks` gate.

## Deferred ideas

Not planned, not tracked in the issue tracker yet.

| Idea | Sketch |
|:--|:--|
| **fish support** | Touches `rcfile`, `initfile` and `platform/shells.go`; needs a `fish.tmpl` per module; `~/.config/fish/config.fish` uses different marker and comment syntax. |
| **Profiles** | A `--profile` flag plus a namespaced `config.toml` section; touches `config` and `ComputePlan`. |
| **Presets / bundles** | Named groups of modules enabled together; a larger change to the config model. |
| **`keychain` (ssh-agent) module** | Not selected so far. |
