#!/bin/sh
# Request auto-merge for pull request $PR in $REPO, but only when main has
# required status checks. Fails closed: if the check count cannot be read, or is
# not a number, auto-merge is not requested and any earlier request is withdrawn.
# Without required checks GitHub would merge as soon as the PR is mergeable.
set -eu

withdraw() {
  gh pr merge "$PR" --repo "$REPO" --disable-auto >/dev/null 2>&1 || true
}

# On an HTTP error gh prints the error body to stdout and exits non-zero, so the
# output is only trusted when the call succeeded.
if ! checks=$(gh api "repos/$REPO/branches/main" \
  --jq '.protection.required_status_checks.checks | length' 2>/dev/null); then
  checks=0
fi
case "$checks" in
  '' | *[!0-9]*) checks=0 ;;
esac

if [ "$checks" -eq 0 ]; then
  echo "::warning::Auto-merge not enabled: main has no required status checks (or they could not be read)."
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "Auto-merge skipped: main has no required status checks (or they could not be read)." >> "$GITHUB_STEP_SUMMARY"
  fi
  withdraw
  exit 0
fi

gh pr merge "$PR" --repo "$REPO" --auto --squash
