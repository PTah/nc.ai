#!/usr/bin/env bash
# Commit + push (counterpart of commit.ps1).
# Usage: ./commit.sh "feat: message"
set -euo pipefail

if [[ $# -lt 1 || -z "${1:-}" ]]; then
  echo "Usage: ./commit.sh \"feat: message\"" >&2
  exit 1
fi

REPO="$(cd "$(dirname "$0")" && pwd)"
MESSAGE="$1"

git -C "$REPO" add -A
if git -C "$REPO" diff --cached --quiet; then
  echo "Nothing to commit."
  exit 0
fi

git -C "$REPO" commit -m "$MESSAGE"
git -C "$REPO" push
echo "Committed and pushed."
