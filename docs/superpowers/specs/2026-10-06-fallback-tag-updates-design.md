# Automated fallback tag updates

Issue: OMNIS-27 (follows OMNIS-26, which pinned every built-in `git` fallback to a release tag).

## Problem

The eleven built-in fallbacks in `modules/builtin/*/manifest.toml` carry a `ref` (a release tag). Without maintenance the tags go stale and fallback builds drift behind upstream. Two further gaps:

- Nothing proposes new tags, and a human has to look up eleven upstreams by hand.
- An existing clone in `~/.config/omnishell/vendor/` is never moved to the pinned tag, because a populated destination counts as satisfied. Users who built from an unpinned clone stay on it forever.

## Goals

- A weekly, reviewable proposal of newer stable tags for the built-in fallbacks.
- Optional, guarded auto-merge of that proposal.
- `omnishell apply` moves an existing vendor clone to the tag the manifest pins.

## Non-goals

- Signature or checksum verification of upstream tags.
- Verifying that a new tag builds (CI does not build the fallbacks).
- Automatic changes to a module's `requires` versions (the PR only reminds the reviewer).
- Updating user-authored modules; only `modules/builtin/` is touched.

## Delivery

Two phases, each its own PR under OMNIS-27, so each can be reviewed and reverted alone:

1. **Phase 1** — tool, workflow, opt-in auto-merge (parts A–C).
2. **Phase 2** — vendor clone refresh in the engine (part D).

## Part A: tag updater tool

`tools/update-fallback-tags/` (Go, `package main`). It lives outside `cmd/omnishell`, so GoReleaser (which builds `./cmd/omnishell` only) does not ship it. It reuses `internal/module.ParseManifest` so the manifest format is not duplicated.

Components, all behind small interfaces so tests need neither network nor real git:

- **Discover** — reads `modules/builtin/*/manifest.toml`; for each module takes `Packages.Fallback[0]` (`repo`, `ref`). A manifest without a `ref` is an error for that module.
- **Latest** — `git ls-remote --tags --refs <repo>` through an injectable `Runner` (the pattern of `internal/pkgmgr`).
- **Select(currentRef, tags)** — a tag is a candidate only if it matches `^v?\d+(\.\d+)+$` **and** uses the same `v`-prefix style as `currentRef`. This excludes `weekly`, `vfox-v…`, pre-releases such as `-rc1`, and upstream's legacy tags (fastfetch has both `2.x` and `v1.0.x`). Versions compare numerically (`2.10` > `2.9`). The result is the highest candidate, and never one lower than `currentRef`; if nothing is higher the module is unchanged.
- **Rewrite** — replaces only the `ref = "…"` line inside the fallback block by text substitution, so comments and formatting survive (no TOML re-marshal).
- **Output** — `--summary <file>` writes a Markdown table (module, old → new). Exit code is 0 even with no change. A failure for one repository is recorded in the summary and does not abort the others.

## Part B: workflow and pull request

`.github/workflows/fallback-tags.yml`, on a weekly `schedule` and `workflow_dispatch`: check out, `setup-go`, run the tool, and if the working tree changed open a PR.

- Branch `chore/update-fallback-tags`, title `chore: update pinned fallback tags`, English body: the summary table plus a reminder to check each module's `requires` against the new tag's toolchain (for example the Rust `rust-version`).
- One PR per run; the fixed branch is updated in place while a PR is still open.
- Token: `secrets.FALLBACK_TAGS_TOKEN || github.token`. A PR created with the default `GITHUB_TOKEN` does not trigger other workflows, so `ci` would not run on it. Configuring a personal access token as `FALLBACK_TAGS_TOKEN` makes `ci` run; without it the workflow still opens the PR.
- Repository setting required: *Allow GitHub Actions to create and approve pull requests* (currently off).
- `permissions`: `contents: write`, `pull-requests: write`; everything else default.

## Part C: opt-in, guarded auto-merge

Auto-merge is requested, with one objection that shapes the design. Today `main` has no branch protection and no rulesets, and the repository setting *Allow auto-merge* is off. GitHub's auto-merge waits for **required** status checks; with none configured it merges at once. In that state "auto-merge" would merge an unreviewed upstream tag with no CI gate. Even with a gate, `ci` does not build the fallbacks, so a green run says nothing about whether a new tag builds.

Design:

- **Off by default.** The workflow enables auto-merge (`gh pr merge --auto --squash`) only when the repository variable `FALLBACK_TAGS_AUTOMERGE` is `true`.
- **Fails closed.** Before enabling it the workflow reads the branch protection of `main` (`gh api repos/{repo}/branches/main/protection`). If `main` has no required status checks, or the call fails, it does not enable auto-merge and records why in the job summary.
- **Same major only.** Auto-merge is requested only when every bump in the PR stays within its compatibility line: the same major version, or the same minor version while the major is 0 (where a minor bump is conventionally breaking). Anything else is left for a human.
- **Withdrawn when it no longer holds.** The PR branch is reused across runs and auto-merge persists on a PR once enabled, so a run that no longer qualifies (a major bump, the variable switched off, no required checks, or a guard that cannot read the protection state) turns auto-merge off again.
- Prerequisites the owner has to set once: enable *Allow auto-merge*, protect `main` with required checks (`test`, `vuln`, `build`), set the variable. Documented in `CONTRIBUTING.md`.

## Part D: vendor clone refresh (phase 2)

Behaviour: when a module's fallback has a `ref` and the existing clone was not built from that `ref`, `omnishell apply` moves the clone to the `ref` and re-runs the build.

- **State.** The lockfile's `ModuleState` gains an optional `fallback_ref` (additive JSON field, `omitempty`), recorded after a successful fallback install. Comparing it with the manifest's `ref` needs no git probe in `ComputePlan`, which stays side-effect-free. A lockfile without the field (every existing install) reads as "unknown", so the clone is refreshed once and then recorded.
- **Plan.** A populated clone whose recorded ref differs from the manifest `ref` becomes a planned fallback *update* instead of "satisfied". The plan output states it, for example `starship fallback: update v1.25.0 → v1.26.0 (rebuild)`, so the existing apply confirmation shows the cost (a `cargo` build can take minutes) before anything runs.
- **Apply.** In the clone: `git fetch --depth 1 origin <ref>`, `git checkout --detach FETCH_HEAD`, then the module's `run`. The hand-edit guard still runs first, and the update happens in the same position as a fresh fallback install.
- **Safety.** If the clone has tracked local changes (`git status --porcelain --untracked-files=no`; untracked `target/` build output is ignored) it is left alone with a note and the module stays healthy on the old version. If fetch, checkout or build fails, the module degrades with the reason and the recorded ref is unchanged, so the next `apply` retries. A failed `cargo install` keeps the previously installed binary.
- **No `ref`, no change.** A fallback without `ref` behaves as today (populated clone = satisfied).
- **Doctor.** An outdated clone makes the module's plan action `update`, so `doctor` reports it through the existing `pending-apply` finding (the one drift code that `doctor --fix` repairs with a re-apply). The misleading `packages-missing` finding is replaced for this case by a `fallback-outdated` notice that names the recorded and the pinned ref.

## Testing

All table-driven, no network.

- Tool: `Select` (highest tag, prefix style, `weekly`/`vfox-v…`/`-rc` ignored, no downgrade, numeric compare, empty list), `Rewrite` (only the `ref` changes, rest preserved, error without `ref`), and a run over a temporary copy of the manifests with a fake `Runner` that checks the summary.
- Workflow guard logic is kept in the tool or a small script with a test where it can be; the YAML itself is verified with a manual `workflow_dispatch`.
- Part D: plan cases (no lock entry, equal ref, different ref, no `ref`), apply cases (success records the ref, dirty clone skipped, fetch/build failure degrades and keeps the ref), and an engine test that `ComputePlan` stays side-effect-free.
- The existing policy test (every built-in fallback has a `ref`) stays.

## Documentation

`CONTRIBUTING.md`: how the workflow runs, how to start it manually, the `FALLBACK_TAGS_TOKEN` secret, the auto-merge prerequisites. Phase 2 also gets a README note on the refresh behaviour and a CHANGELOG entry (it is user-visible); the CHANGELOG line from OMNIS-26 that says existing clones are kept is updated.

## Decisions for review

- **Auto-merge** is included but off by default and guarded (part C). The alternative is to leave it out entirely.
- **Vendor refresh** is included as a separate phase. Its cost: after upgrading, the next `apply` rebuilds every fallback-built tool once, which can take minutes; the plan and the confirmation make that visible first.
- **Tag-to-`requires` consistency** stays a manual check; a new tag may need a higher toolchain than the manifest declares.
