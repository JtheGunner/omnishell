#!/bin/sh
# Refuse to release a tag whose commit is not part of main's history.
#
# `on: push: tags` cannot filter by branch, so anyone who can push a tag could
# otherwise publish a release (and a Homebrew formula) from an unmerged branch.
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
