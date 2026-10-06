#!/bin/sh
# Refuse to release a tag whose commit is not part of main's history.
#
# `on: push: tags` cannot filter by branch, so a tag pushed on an unmerged
# branch would otherwise publish a release (and a Homebrew formula). This
# guards against mistakes only: a tag push runs the workflow file of the tagged
# commit, so someone who can push a branch can also edit this check away. Who
# may create release tags is a repository setting; restrict `v*` tag creation
# with a tag ruleset. Ancestry is deliberate: it also allows a tag on an older
# commit of main (a hotfix); it does not prove the commit is main's tip.
#
# Fails closed: if origin/main is not available the tag cannot be confirmed.
set -eu

branch="${RELEASE_BRANCH:-main}"

if ! git rev-parse --verify --quiet "refs/remotes/origin/$branch" >/dev/null; then
  echo "::error::origin/$branch is not available; cannot confirm that tag ${GITHUB_REF_NAME:-?} is on $branch."
  exit 1
fi

if ! git merge-base --is-ancestor "$GITHUB_SHA" "refs/remotes/origin/$branch"; then
  echo "::error::Tag ${GITHUB_REF_NAME:-?} ($GITHUB_SHA) is not on $branch; refusing to release."
  exit 1
fi
