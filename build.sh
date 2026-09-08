#!/usr/bin/env bash
# Build NotCursor.app for macOS (counterpart of build.ps1).
# Output: build/bin/NotCursor.app (canonical). /tmp is only a staging area for codesign.
# Usage:
#   ./build.sh              # build + restart
#   ./build.sh --no-restart # build only
#   ./build.sh --universal  # fat binary (arm64+amd64)
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="NotCursor.app"
BIN_DIR="$REPO/build/bin"
APP_PATH="$BIN_DIR/$APP_NAME"
LOG="$(mktemp -t nc-wails-build.XXXXXX)"
STAGE_DIR=""

NO_RESTART=0
UNIVERSAL=0
for arg in "$@"; do
  case "$arg" in
    --no-restart|-NoRestart) NO_RESTART=1 ;;
    --universal|-Universal) UNIVERSAL=1 ;;
    -h|--help)
      cat <<'EOF'
Usage: ./build.sh [--no-restart] [--universal]

  --no-restart   Build and sign only; do not quit/relaunch NotCursor.
  --universal    Build darwin/universal instead of host arch.

Artifact: build/bin/NotCursor.app
EOF
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

cleanup() {
  rm -f "$LOG"
  if [[ -n "$STAGE_DIR" && -d "$STAGE_DIR" ]]; then
    rm -rf "$STAGE_DIR"
  fi
}
trap cleanup EXIT

export PATH="/opt/homebrew/bin:/usr/local/go/bin:$(go env GOPATH 2>/dev/null)/bin:${PATH:-}"
# proxy.golang.org often RSTs on this LAN — keep fallbacks.
export GOPROXY="${GOPROXY:-https://proxy.golang.org,https://goproxy.io,https://goproxy.cn,direct}"

if ! command -v go >/dev/null 2>&1; then
  echo "go not found in PATH" >&2
  exit 1
fi
if ! command -v wails >/dev/null 2>&1; then
  echo "wails not found — install: go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0" >&2
  exit 1
fi

ARCH="$(uname -m)"
if [[ "$UNIVERSAL" -eq 1 ]]; then
  PLATFORM="darwin/universal"
elif [[ "$ARCH" == "arm64" ]]; then
  PLATFORM="darwin/arm64"
else
  PLATFORM="darwin/amd64"
fi

# Wails rejects UTF-8 BOM in wails.json.
python3 - "$REPO/wails.json" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
if p.is_file():
    p.write_bytes(p.read_text(encoding="utf-8-sig").encode("utf-8"))
PY

cd "$REPO"

echo "Fetching Go modules (GOPROXY=$GOPROXY)…"
if ! go mod download; then
  echo "go mod download failed — check network / GOPROXY" >&2
  exit 1
fi

# Drop previous bundle so a failed compile cannot be mistaken for success.
rm -rf "$APP_PATH"

echo "Building ($PLATFORM)…"
set +e
wails build -platform "$PLATFORM" 2>&1 | tee "$LOG"
WAILS_RC=${PIPESTATUS[0]}
set -e

if [[ ! -d "$APP_PATH" ]]; then
  echo "Build failed: missing $APP_PATH (wails exit $WAILS_RC)" >&2
  exit 1
fi

# We deleted $APP_PATH before the build. If it exists again, packaging succeeded.
# Wails often then fails codesign inside Documents (xattrs / Finder info).
if [[ "$WAILS_RC" -ne 0 ]]; then
  LOG_PLAIN="$(sed $'s/\033\\[[0-9;]*[[:alpha:]]//g' "$LOG" 2>/dev/null || cat "$LOG")"
  if printf '%s\n' "$LOG_PLAIN" | grep -qiE 'codesign failed|resource fork|Finder information|Self-signing application'; then
    echo "wails in-tree codesign failed (Documents xattrs) — will adhoc-sign into $APP_PATH…"
  elif printf '%s\n' "$LOG_PLAIN" | grep -q 'Packaging application: Done'; then
    echo "wails exited $WAILS_RC after packaging — will adhoc-sign into $APP_PATH…"
  else
    echo "wails exited $WAILS_RC but $APP_NAME exists — will adhoc-sign into $APP_PATH…"
  fi
fi

# Stage outside Documents only for codesign; final artifact is always build/bin.
STAGE_DIR="$(mktemp -d -t nc-app-sign.XXXXXX)"
STAGE_APP="$STAGE_DIR/$APP_NAME"
echo "Adhoc codesign (staging $STAGE_DIR → $APP_PATH)…"
xattr -cr "$APP_PATH" 2>/dev/null || true
find "$APP_PATH" -type f -exec xattr -c {} \; 2>/dev/null || true
ditto --norsrc --noextattr --noacl "$APP_PATH" "$STAGE_APP"
xattr -cr "$STAGE_APP" 2>/dev/null || true
codesign --force --deep --sign - "$STAGE_APP"
rm -rf "$APP_PATH"
mkdir -p "$BIN_DIR"
ditto --norsrc --noextattr --noacl "$STAGE_APP" "$APP_PATH"
rm -rf "$STAGE_DIR"
STAGE_DIR=""
codesign --verify --deep --strict "$APP_PATH" 2>/dev/null || true
echo "Build finished: $APP_PATH"

if [[ "$NO_RESTART" -eq 1 ]]; then
  echo "Done (no restart)."
  exit 0
fi

echo "Restarting NotCursor from $APP_PATH…"
pkill -x NotCursor 2>/dev/null || true
for _ in $(seq 1 40); do
  if ! pgrep -x NotCursor >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done
open "$APP_PATH"
echo "Launched: $APP_PATH"
exit 0
