//go:build darwin

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// swapScript ждёт выхода приложения, подменяет .app-бандл и открывает его.
// На macOS подмена запущенного бандла допустима (POSIX разрешает замену
// файлов), но делаем это после выхода процесса, чтобы не оставить полуфасад.
const swapScript = `#!/bin/bash
# $1 — pid приложения, $2 — распакованный бандл, $3 — установленный бандл, $4 — лог
PID="$1"; NEW="$2"; APP="$3"; LOG="$4"
note() { [ -n "$LOG" ] && echo "$(date -u +%FT%TZ) $*" >> "$LOG"; }
note "waiting for pid $PID"
for _ in $(seq 1 60); do
  kill -0 "$PID" 2>/dev/null || break
  sleep 0.5
done
kill -9 "$PID" 2>/dev/null
sleep 1
BACKUP="$APP.bak"
rm -rf "$BACKUP"
if [ -e "$APP" ]; then mv "$APP" "$BACKUP"; fi
if ditto "$NEW" "$APP"; then
  note "bundle replaced"
  rm -rf "$BACKUP"
else
  note "ditto failed, restoring"
  rm -rf "$APP"
  [ -e "$BACKUP" ] && mv "$BACKUP" "$APP"
fi
open "$APP"
`

// spawnSwap пишет shell-скрипт подмены и запускает его в отдельной сессии,
// чтобы он пережил выход приложения.
func spawnSwap(stage, unpacked, appExe string) error {
	newApp, ok := findMacApp(unpacked)
	if !ok {
		return errors.New("в архиве обновления не найден NotCursor.app")
	}
	// appExe = <bundle>/Contents/MacOS/NotCursor → бандл на три уровня выше.
	app := filepath.Dir(filepath.Dir(filepath.Dir(appExe)))
	if filepath.Ext(app) != ".app" {
		return fmt.Errorf("не удалось определить .app-бандл (получено %q)", app)
	}
	script := filepath.Join(stage, "swap.sh")
	if err := os.WriteFile(script, []byte(swapScript), 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(stage, "swap.log")
	cmd := exec.Command("/bin/bash", script, fmt.Sprint(os.Getpid()), newApp, app, logPath)
	cmd.Dir = stage
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
