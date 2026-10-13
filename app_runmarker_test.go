// Created by nc.ai 0.8.6.1 on macbook 2026-10-13 22:44:43
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Маркер прогона должен существовать ровно столько, сколько идёт работа:
// build.sh и скрипты перезапуска читают running.json и по нему решают, можно ли
// закрывать приложение. Регрессия 13.10.2026: маркер ставился и снимался в
// синхронной части RunAgentInSession, а сам прогон уходил в горутину — файл
// удалялся через миллисекунды, и «идёт прогон» не видел никто.
func TestRunMarkerLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := NewApp()
	if path, err := runMarkerPath(); err != nil || path == "" {
		t.Fatalf("runMarkerPath: %v", err)
	}
	path, _ := runMarkerPath()

	a.markRunStart("sess-1")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("маркер не создан: %v", err)
	}
	var marker struct {
		PID      int      `json:"pid"`
		Sessions []string `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		t.Fatalf("маркер не JSON: %v", err)
	}
	if len(marker.Sessions) != 1 || marker.Sessions[0] != "sess-1" {
		t.Fatalf("сессии в маркере: %v", marker.Sessions)
	}
	if marker.PID != os.Getpid() {
		t.Fatalf("pid в маркере %d, ожидали %d", marker.PID, os.Getpid())
	}

	// Вторая дорожка в той же сессии: файл остаётся, пока жива хоть одна.
	a.markRunStart("sess-1")
	a.markRunEnd("sess-1")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("маркер исчез, хотя одна дорожка ещё идёт: %v", err)
	}

	a.markRunStart("sess-2")
	a.markRunEnd("sess-2")
	a.markRunEnd("sess-1")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("маркер должен быть удалён после последнего прогона (err=%v)", err)
	}
	// Директория данных создаётся по пути маркера — проверим, что это та же
	// папка, что читают скрипты перезапуска.
	if filepath.Base(filepath.Dir(path)) != "NotCursor" {
		t.Fatalf("путь маркера %q, ожидали …/NotCursor/running.json", path)
	}
}
