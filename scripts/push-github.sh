#!/usr/bin/env bash
# Зеркало master → github: ОБЫЧНЫЙ push, как в приватные зеркала.
# Историю не переписываем, sanitize/orphan не делаем: секретов в проекте нет,
# в дерево попадают только код, доки и ссылки на репозитории.
# Использование: scripts/push-github.sh [branch] [remote]
set -euo pipefail

# В агентских и CI-оболочках на Windows HOME=/home/<user> и USERPROFILE пустой:
# ssh тогда не находит ~/.ssh/config, не видит IdentityFile для github.com и
# перебирает дефолтные ключи — push падает с "Permission denied (publickey)".
# Берём профиль Windows из HOMEDRIVE+HOMEPATH: никаких личных путей в коде
# (на macOS/Linux cygpath отсутствует и строка пропускается).
win_home="${USERPROFILE:-${HOMEDRIVE:-}${HOMEPATH:-}}"
if command -v cygpath >/dev/null 2>&1 && [ -n "$win_home" ]; then
  export HOME="$(cygpath -u "$win_home")"
fi

BRANCH="${1:-master}"
REMOTE="${2:-github}"

git push "$REMOTE" "$BRANCH"
echo "pushed ${BRANCH} to ${REMOTE}"
