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
A remote registry would add a third source. A module's `zsh.tmpl` / `bash.tmpl`
is shell code that runs in every new shell, so a registry is a code-distribution
channel: authenticity and honesty about residual risk matter more than
convenience. This section records the proposed design; nothing here is built.

- **Hosting:** this project operates no registry. A registry is any HTTPS
  location that serves a signed `index.toml` and one archive per module
  version. The user adds it explicitly (`[registries]` in `config.toml`); there
  is no default registry.
- **Authenticity:** the index is signed with Ed25519 (detached
  `index.toml.sig`; the algorithm is in the Go standard library, so no new
  dependency). The user pins the registry's public key in `config.toml` next to
  its URL: trust comes from explicit configuration, never from first use. The
  index lists each archive's SHA-256. The archive is verified against its index
  entry before anything is extracted; a failed signature or checksum stops the
  install and leaves existing module directories untouched.
- **Replay, downgrade, freeze:** the signed index carries a monotonically
  increasing `sequence` and an `expires` timestamp. A client rejects an index
  with a lower sequence than the last one it saw, an expired index, and an
  archive whose version is lower than the installed one unless the user asks
  for it. Without this a stale but validly signed index could serve an
  archive with a known vulnerability.
- **Index validation:** module ids must match the manifest id pattern and are
  never used as a path unless they do. Archive URLs are relative to the registry
  URL, HTTPS only, and must stay on the registry's host (no redirect to another
  host).
- **Archive extraction:** this is new code. `internal/selfupdate` reads a
  single member into a writer and is not enough: a module archive holds several
  files. Rules: extract into a temp directory next to the target, accept only
  regular files named `manifest.toml`, `*.tmpl` and `hooks/*` (no symlinks, no
  hardlinks, no devices), reject absolute and `..` names, cap file count and
  total size, then swap the directory in with a rename.
- **Key management:** owned by whoever runs a registry. A registry may publish
  several keys so users can rotate without a flag day (add the new key, then
  drop the old one). There is no remote revocation: a compromised key means
  every user edits their config. This is a known limit of pinning without a
  central authority.
- **Storage and ownership:** an installed module is copied to
  `~/.config/omnishell/modules/<id>/` with a `.registry.toml` record (registry
  URL, version, archive SHA-256, install-time choices). That file is also the
  proof of ownership: `registry install` and `registry update` only replace a
  directory that has a matching `.registry.toml`. An id that exists as a
  hand-written local module is refused, so a registry can never overwrite local
  work.
- **Built-in id collision:** refused unless the user passes `--override`. The
  existing override rule (`Registry.Overrides()`) and the `doctor` notice then
  apply unchanged.
- **Privileges, honestly:** a registry module can ship arbitrary shell
  snippets, packages (installed with the package manager, possibly via sudo),
  hooks and fallbacks. Snippets cannot be restricted: they are the module. So
  `--allow-hooks` is a speed bump for hooks and git/release fallbacks, not a
  sandbox, and the doc and the CLI must say so. Before the user confirms an
  install or an update, omnishell prints the packages, hooks, fallbacks and the
  full text of the templates (for an update, a diff against the installed
  copy).
- **Enforcing the hooks gate:** unlike a plain user module this needs a new code
  path. The module registry loads `.registry.toml`, marks the module as remote
  and, unless hooks were allowed, drops its hooks and fallbacks when the plan is
  computed, so the restriction is in place before `Apply` and its hand-edit
  guard. `registry update` keeps the recorded choice and asks again if the new
  version adds hooks or fallbacks the old one did not have.
- **Integrity after install:** the init-file hand-edit guard and `rollback` do
  not cover `modules/<id>/`; rollback restores init files, the rc file and the
  lockfile. A tampered local copy of a registry module would go unnoticed, so
  `doctor` should compare each registry module's directory with the recorded
  hash. Rolling a module back to an older version is `registry install` of that
  version, not `rollback`.
- **Offline behaviour:** only `registry install` and `registry update` use the
  network. `apply`, `doctor` and the other commands read the local copy, so
  built-in and local modules behave as before without a network.
- **Updates:** explicit only (`registry update <id>`); never during `apply`.
- **Out of scope:** operating a public registry, ratings, search.

Suggested build order: signature, index and checksum verification as a
standalone package; safe archive extraction; `registry install` writing a user
module with `.registry.toml`; the hooks gate and `doctor` check; `registry
update`.

## Deferred ideas

Not planned, not tracked in the issue tracker yet.

| Idea | Sketch |
|:--|:--|
| **fish support** | Touches `rcfile`, `initfile` and `platform/shells.go`; needs a `fish.tmpl` per module; `~/.config/fish/config.fish` uses different marker and comment syntax. |
| **Profiles** | A `--profile` flag plus a namespaced `config.toml` section; touches `config` and `ComputePlan`. |
| **Presets / bundles** | Named groups of modules enabled together; a larger change to the config model. |
| **`keychain` (ssh-agent) module** | Not selected so far. |
