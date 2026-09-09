#!/usr/bin/env bash
# Push public mirror to github WITHOUT internal docs/TODO-*.md
# Private remotes (home, kalinamall) keep ToDo files.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
BRANCH="${1:-master}"
REMOTE="github"

cd "${REPO}"

if ! git remote get-url "${REMOTE}" >/dev/null 2>&1; then
  echo "remote '${REMOTE}' not configured" >&2
  exit 1
fi

# Refuse if working tree dirty — avoid publishing half-baked state.
if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree not clean; commit or stash first" >&2
  exit 1
fi

WORK="$(mktemp -d -t nc-github-push.XXXXXX)"
cleanup() { rm -rf "${WORK}"; }
trap cleanup EXIT

git clone --shared --no-checkout "${REPO}" "${WORK}/repo"
cd "${WORK}/repo"
git checkout -q "${BRANCH}"

# Shared clone has no remotes from the source — add github by URL.
GITHUB_URL="$(git -C "${REPO}" remote get-url "${REMOTE}")"
git remote add "${REMOTE}" "${GITHUB_URL}"
git fetch --prune "${REMOTE}" "${BRANCH}"

shopt -s nullglob
TODOS=(docs/TODO-*.md)
if ((${#TODOS[@]} > 0)); then
  git rm -f -- "${TODOS[@]}"
  git commit -m "chore: strip internal TODO docs from public github mirror"
else
  echo "no docs/TODO-*.md on ${BRANCH} (already clean)"
fi

# Public tip intentionally diverges (no ToDo files). Lease against fetched tip.
git push --force-with-lease="${BRANCH}:refs/remotes/${REMOTE}/${BRANCH}" "${REMOTE}" "HEAD:${BRANCH}"
echo "Pushed ${BRANCH} to ${REMOTE} without docs/TODO-*.md"
