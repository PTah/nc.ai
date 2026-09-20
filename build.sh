#!/usr/bin/env bash
# Build NotCursor.app for macOS (counterpart of build.ps1).
# Output: build/bin/NotCursor.app (canonical). /tmp is only a staging area for codesign.
# Usage:
#   ./build.sh              # build + restart
#   ./build.sh --no-restart # build only
#   ./build.sh --universal  # fat binary (arm64+amd64)
#   ./build.sh --copy-to /path/to/share   # also copy .app into folder
#   ./build.sh --no-install               # do not touch the installed copy
#
# By default a successful build REPLACES ${INSTALL_DIR}/NotCursor.app (/Applications) and
# relaunches the app from there, so launching NotCursor from Finder/Spotlight always runs
# the newest build. Change the target with --install-to DIR or $NC_INSTALL_DIR.
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
INSTALL_DIR="${NC_INSTALL_DIR:-/Applications}"
DO_INSTALL=1

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
    --install-to|-InstallTo)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "Missing path after $1" >&2
        exit 2
      fi
      INSTALL_DIR="$2"
      shift 2
      ;;
    --install-to=*|-InstallTo=*)
      INSTALL_DIR="${1#*=}"
      shift
      ;;
    --no-install|-NoInstall) DO_INSTALL=0; shift ;;
    -h|--help)
      cat <<EOF
Usage: ./build.sh [--no-restart] [--universal] [--install-to DIR] [--no-install] [--copy-to DIR]

  --no-restart     Build and sign only; do not quit/relaunch NotCursor.
  --universal      Build darwin/universal instead of host arch.
  --install-to DIR Replace DIR/NotCursor.app with the fresh bundle and relaunch
                   from there (default: /Applications, or \$NC_INSTALL_DIR).
  --no-install     Leave DIR untouched; artifact stays in build/bin.
  --copy-to DIR    Also copy NotCursor.app into DIR (e.g. network share).

Artifact: build/bin/NotCursor.app (plus ${INSTALL_DIR}/NotCursor.app by default)
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

# Read the version straight from the bundle (used before and after the build).
app_version() {
  /usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$1/Contents/Info.plist" 2>/dev/null || echo "?"
}

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
echo "Build finished: ${APP_PATH} (v$(app_version "${APP_PATH}"))"

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

quit_app() {
  [[ -z "$(app_pids)" ]] && return 0
  echo "Stopping running NotCursor (pid: $(printf '%s ' $(app_pids)))..."
  # `osascript ... to quit` would launch the app if nothing is running, so it only runs here.
  osascript -e 'tell application "NotCursor" to quit' >/dev/null 2>&1 || true
  for _ in $(seq 1 40); do
    [[ -z "$(app_pids)" ]] && return 0
    sleep 0.25
  done
  echo "NotCursor did not quit - sending TERM" >&2
  kill $(app_pids) 2>/dev/null || true
  sleep 1
  if [[ -n "$(app_pids)" ]]; then
    kill -9 $(app_pids) 2>/dev/null || true
    sleep 0.5
  fi
  return 0
}

# Replace the copy launched from Finder/Spotlight so it is always the newest build.
INSTALLED_APP=""
if [[ "${DO_INSTALL}" -eq 1 ]]; then
  INSTALL_APP="${INSTALL_DIR%/}/${APP_NAME}"
  INSTALL_PARENT="$(dirname "${INSTALL_APP}")"
  if [[ ! -d "${INSTALL_PARENT}" ]]; then
    echo "Install skipped: ${INSTALL_PARENT} not found" >&2
  elif [[ ! -w "${INSTALL_PARENT}" ]]; then
    echo "Install skipped: no write permission for ${INSTALL_PARENT}" >&2
  else
    [[ "${NO_RESTART}" -eq 1 ]] || quit_app
    if [[ -n "$(app_pids)" ]]; then
      echo "NotCursor is still running - replacing the bundle on disk anyway" >&2
    fi
    rm -rf "${INSTALL_APP}"
    ditto --norsrc --noextattr --noacl "${APP_PATH}" "${INSTALL_APP}"
    xattr -dr com.apple.quarantine "${INSTALL_APP}" 2>/dev/null || true
    if codesign --verify --deep --strict "${INSTALL_APP}" 2>/dev/null; then
      INSTALLED_APP="${INSTALL_APP}"
      echo "Installed: ${INSTALL_APP} (v$(app_version "${INSTALL_APP}"))"
    else
      echo "Installed bundle failed codesign verify - removing ${INSTALL_APP}" >&2
      rm -rf "${INSTALL_APP}"
    fi
  fi
fi

TARGET="${INSTALLED_APP:-${APP_PATH}}"
APP_VERSION="$(app_version "${TARGET}")"

if [[ "${NO_RESTART}" -eq 1 ]]; then
  echo "Done (no restart). Artifact: ${APP_PATH} (v$(app_version "${APP_PATH}"))"
  if [[ -n "${INSTALLED_APP}" && -n "$(app_pids)" ]]; then
    echo "Running NotCursor still uses the previous binary - relaunch it manually." >&2
  fi
  exit 0
fi

RUNNING_PIDS="$(app_pids)"
if [[ -z "${RUNNING_PIDS}" ]]; then
  open "${TARGET}"
  echo "Launched: ${TARGET} (v${APP_VERSION})"
  exit 0
fi

echo "Restarting NotCursor from ${TARGET} (pid: $(printf '%s ' ${RUNNING_PIDS}))..."
INSIDE=0
if inside_app; then
  # Build started from the terminal panel inside NotCursor: quitting the app kills this
  # shell together with the PTY, so the relaunch goes to a detached process first.
  INSIDE=1
  nohup /bin/sh -c "sleep 2; open '${TARGET}'" >/dev/null 2>&1 &
  disown 2>/dev/null || true
  echo "Detached relaunch scheduled (this shell dies with the app)."
fi

quit_app

if [[ "${INSIDE}" -eq 0 ]]; then
  open "${TARGET}"
fi
echo "Launched: ${TARGET} (v${APP_VERSION})"
exit 0
