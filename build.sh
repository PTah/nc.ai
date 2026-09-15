!/usr/bin/env bash
# Build NotCursor.app for macOS (counterpart of build.ps1).
# Output: build/bin/NotCursor.app (canonical). /tmp is only a staging area for codesign.
# Usage:
#   ./build.sh              # build + restart
#   ./build.sh --no-restart # build only
#   ./build.sh --universal  # fat binary (arm64+amd64)
#   ./build.sh --copy-to /path/to/share   # also copy .app into folder
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="NotCursor.app"
BIN_DIR="${REPO}/build/bin"
APP_PATH="${BIN_DIR}/${APP_NAME}"
LOG="$(mktemp -t nc-wails-build.XXXXXX)"
STAGE_DIR=""

NO_RESTART=0
UNIVERSAL=0
COPY_TO=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-restart|-NoRestart) NO_RESTART=1; shift ;;
    --universal|-Universal) UNIVERSAL=1; shift ;;
    --copy-to|-CopyTo)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "Missing path after $1" >&2
        exit 2
      fi
      COPY_TO="$2"
      shift 2
      ;;
    --copy-to=*|-CopyTo=*)
      COPY_TO="${1#*=}"
      shift
      ;;
    -h|--help)
      cat <<'EOF'
Usage: ./build.sh [--no-restart] [--universal] [--copy-to DIR]

  --no-restart   Build and sign only; do not quit/relaunch NotCursor.
  --universal    Build darwin/universal instead of host arch.
  --copy-to DIR  After a successful build, copy NotCursor.app into DIR
                 (e.g. network share). If omitted, artifact stays in build/bin.

Artifact: build/bin/NotCursor.app
EOF
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

cleanup() {
  rm -f "${LOG}"
  if [[ -n "${STAGE_DIR}" && -d "${STAGE_DIR}" ]]; then
    rm -rf "${STAGE_DIR}"
  fi
}
trap cleanup EXIT

export PATH="/opt/homebrew/bin:/usr/local/go/bin:$(go env GOPATH 2>/dev/null)/bin:${PATH:-}"
# proxy.golang.org often RSTs on this LAN - keep fallbacks.
export GOPROXY="${GOPROXY:-https://proxy.golang.org,https://goproxy.io,https://goproxy.cn,direct}"

if ! command -v go >/dev/null 2>&1; then
  echo "go not found in PATH" >&2
  exit 1
fi
if ! command -v wails >/dev/null 2>&1; then
  echo "wails not found - install: go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0" >&2
  exit 1
fi

ARCH="$(uname -m)"
if [[ "${UNIVERSAL}" -eq 1 ]]; then
  PLATFORM="darwin/universal"
elif [[ "${ARCH}" == "arm64" ]]; then
  PLATFORM="darwin/arm64"
else
  PLATFORM="darwin/amd64"
fi

# Wails rejects UTF-8 BOM in wails.json.
python3 - "${REPO}/wails.json" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
if p.is_file():
    p.write_bytes(p.read_text(encoding="utf-8-sig").encode("utf-8"))
PY

cd "${REPO}"

echo "Fetching Go modules (GOPROXY=${GOPROXY})..."
if ! go mod download; then
  echo "go mod download failed - check network / GOPROXY" >&2
  exit 1
fi

# Drop previous bundle so a failed compile cannot be mistaken for success.
rm -rf "${APP_PATH}"

echo "Building (${PLATFORM})..."
set +e
wails build -platform "${PLATFORM}" 2>&1 | tee "${LOG}"
WAILS_RC=${PIPESTATUS[0]}
set -e

if [[ ! -d "${APP_PATH}" ]]; then
  echo "Build failed: missing ${APP_PATH} (wails exit ${WAILS_RC})" >&2
  exit 1
fi

# We deleted ${APP_PATH} before the build. If it exists again, packaging succeeded.
# Wails often then fails codesign inside Documents (xattrs / Finder info).
if [[ "${WAILS_RC}" -ne 0 ]]; then
  LOG_PLAIN="$(sed $'s/\033\\[[0-9;]*[[:alpha:]]//g' "${LOG}" 2>/dev/null || cat "${LOG}")"
  if printf '%s\n' "${LOG_PLAIN}" | grep -qiE 'codesign failed|resource fork|Finder information|Self-signing application'; then
    echo "wails in-tree codesign failed (Documents xattrs) - will adhoc-sign into ${APP_PATH}..."
  elif printf '%s\n' "${LOG_PLAIN}" | grep -q 'Packaging application: Done'; then
    echo "wails exited ${WAILS_RC} after packaging - will adhoc-sign into ${APP_PATH}..."
  else
    echo "wails exited ${WAILS_RC} but ${APP_NAME} exists - will adhoc-sign into ${APP_PATH}..."
  fi
fi

# Stage outside Documents only for codesign; final artifact is always build/bin.
STAGE_DIR="$(mktemp -d -t nc-app-sign.XXXXXX)"
STAGE_APP="${STAGE_DIR}/${APP_NAME}"
echo "Adhoc codesign (staging ${STAGE_DIR} -> ${APP_PATH})..."
xattr -cr "${APP_PATH}" 2>/dev/null || true
find "${APP_PATH}" -type f -exec xattr -c {} \; 2>/dev/null || true
ditto --norsrc --noextattr --noacl "${APP_PATH}" "${STAGE_APP}"
xattr -cr "${STAGE_APP}" 2>/dev/null || true
codesign --force --deep --sign - "${STAGE_APP}"
rm -rf "${APP_PATH}"
mkdir -p "${BIN_DIR}"
ditto --norsrc --noextattr --noacl "${STAGE_APP}" "${APP_PATH}"
rm -rf "${STAGE_DIR}"
STAGE_DIR=""
codesign --verify --deep --strict "${APP_PATH}" 2>/dev/null || true
echo "Build finished: ${APP_PATH}"

if [[ -n "${COPY_TO}" ]]; then
  mkdir -p "${COPY_TO}"
  DEST="${COPY_TO%/}/${APP_NAME}"
  rm -rf "${DEST}"
  ditto --norsrc --noextattr --noacl "${APP_PATH}" "${DEST}"
  echo "Copied to: ${DEST}"
fi

# PIDs of the running bundle. `pkill -x NotCursor` is not enough: a process started with a
# relative path gets its name truncated to 16 chars ("build/bin/NotCur"), so match the
# bundle path inside `ps` output instead.
app_pids() {
  ps -Ao pid=,command= | awk '/NotCursor\.app\/Contents\/MacOS\/NotCursor/ {print $1}'
}

inside_app() {
  local pid="$$" live
  live="$(app_pids)"
  [[ -z "${live}" ]] && return 1
  while [[ -n "${pid}" && "${pid}" != "1" ]]; do
    if printf '%s\n' "${live}" | grep -qx "${pid}"; then
      return 0
    fi
    pid="$(ps -o ppid= -p "${pid}" 2>/dev/null | tr -d ' ')"
  done
  return 1
}

if [[ "${NO_RESTART}" -eq 1 ]]; then
  echo "Done (no restart)."
  exit 0
fi

RUNNING_PIDS="$(app_pids)"
if [[ -z "${RUNNING_PIDS}" ]]; then
  open "${APP_PATH}"
  echo "Launched: ${APP_PATH}"
  exit 0
fi

echo "Restarting NotCursor from ${APP_PATH} (pid: $(printf '%s ' ${RUNNING_PIDS}))..."
INSIDE=0
if inside_app; then
  # Build started from the terminal panel inside NotCursor: quitting the app kills this
  # shell together with the PTY, so the relaunch goes to a detached process first.
  INSIDE=1
  nohup /bin/sh -c "sleep 2; open '${APP_PATH}'" >/dev/null 2>&1 &
  disown 2>/dev/null || true
  echo "Detached relaunch scheduled (this shell dies with the app)."
fi

osascript -e 'tell application "NotCursor" to quit' >/dev/null 2>&1 || true
for _ in $(seq 1 40); do
  if [[ -z "$(app_pids)" ]]; then
    break
  fi
  sleep 0.25
done
if [[ -n "$(app_pids)" ]]; then
  echo "NotCursor did not quit - sending TERM" >&2
  kill $(app_pids) 2>/dev/null || true
  sleep 1
fi

if [[ "${INSIDE}" -eq 0 ]]; then
  open "${APP_PATH}"
fi
echo "Launched: ${APP_PATH}"
exit 0
