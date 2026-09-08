#!/usr/bin/env bash
# Build NotCursor.app for macOS (counterpart of build.ps1).
# Default: build darwin/<host-arch>, adhoc-sign, (re)launch the app.
# Usage:
#   ./build.sh              # build + restart
#   ./build.sh --no-restart # build only
#   ./build.sh --universal  # fat binary (arm64+amd64)
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="NotCursor.app"
BIN_DIR="$REPO/build/bin"
APP_PATH="$BIN_DIR/$APP_NAME"
TMP_APP="/tmp/$APP_NAME"

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
EOF
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

export PATH="/opt/homebrew/bin:/usr/local/go/bin:$(go env GOPATH 2>/dev/null)/bin:${PATH:-}"

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

echo "Building ($PLATFORM)…"
# codesign inside Documents often fails (xattrs); package still lands in build/bin.
set +e
wails build -platform "$PLATFORM"
WAILS_RC=$?
set -e
if [[ ! -d "$APP_PATH" ]]; then
  echo "Build failed: missing $APP_PATH (wails exit $WAILS_RC)" >&2
  exit 1
fi
if [[ "$WAILS_RC" -ne 0 ]]; then
  echo "wails reported exit $WAILS_RC (often codesign in Documents) — signing via /tmp…"
fi

echo "Adhoc codesign via clean /tmp copy…"
rm -rf "$TMP_APP"
ditto --norsrc --noextattr --noacl "$APP_PATH" "$TMP_APP"
xattr -cr "$TMP_APP" 2>/dev/null || true
codesign --force --deep --sign - "$TMP_APP"
rm -rf "$APP_PATH"
ditto --norsrc --noextattr --noacl "$TMP_APP" "$APP_PATH"
echo "Build finished: $APP_PATH"
echo "Also signed:    $TMP_APP"

if [[ "$NO_RESTART" -eq 1 ]]; then
  echo "Done (no restart)."
  exit 0
fi

echo "Restarting NotCursor…"
pkill -x NotCursor 2>/dev/null || true
for _ in $(seq 1 40); do
  if ! pgrep -x NotCursor >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done
open "$TMP_APP"
echo "Launched: $TMP_APP"
exit 0
