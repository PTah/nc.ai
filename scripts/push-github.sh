#!/usr/bin/env bash
# Зеркало master → github: ОБЫЧНЫЙ push, как в home и kalinamall.
# Историю не переписываем, sanitize/orphan не делаем: секретов в проекте нет,
# в дерево попадают только код, доки и ссылки на репозитории.
# Использование: scripts/push-github.sh [branch] [remote]
set -euo pipefail

BRANCH="${1:-master}"
REMOTE="${2:-github}"

git push "$REMOTE" "$BRANCH"
echo "pushed ${BRANCH} to ${REMOTE}"
