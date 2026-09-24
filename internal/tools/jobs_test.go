package tools

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func sleepCommand(seconds int) string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Seconds " + itoa(seconds)
	}
	return "sleep " + itoa(seconds)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// Фоновая команда не должна висеть после своего лимита.
func TestJobStoreTimeout(t *testing.T) {
	store := NewJobStore()
	id, err := store.Start(sleepCommand(30), "", "", time.Second)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	start := time.Now()
	out, err := store.Status(id, 20, 4000, "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "status=done") {
		t.Fatalf("job не завершился по лимиту:\n%s", out)
	}
	if !strings.Contains(out, "command timed out") {
		t.Errorf("в выводе нет причины таймаута:\n%s", out)
	}
	if elapsed := time.Since(start); elapsed > 25*time.Second {
		t.Fatalf("таймаут сработал слишком поздно: %s", elapsed)
	}
}

// При выходе из приложения запущенные команды гасим, а не оставляем в системе.
func TestJobStoreKillAll(t *testing.T) {
	store := NewJobStore()
	id, err := store.Start(sleepCommand(60), "", "", 5*time.Minute)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	store.KillAll()
	out, err := store.Status(id, 20, 4000, "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "status=done") || !strings.Contains(out, "отменена") {
		t.Fatalf("job не остановлен:\n%s", out)
	}
}

// Завершённые jobs не копятся бесконечно: держим только последние.
func TestJobStorePrunesFinished(t *testing.T) {
	store := NewJobStore()
	ids := make([]string, 0, maxFinishedJobs+5)
	for i := 0; i < maxFinishedJobs+5; i++ {
		id, err := store.Start("echo done", "", "", 10*time.Second)
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		ids = append(ids, id)
		if _, err := store.Status(id, 10, 500, ""); err != nil {
			t.Fatalf("status %d: %v", i, err)
		}
	}
	store.mu.Lock()
	kept := len(store.jobs)
	store.mu.Unlock()
	if kept > maxFinishedJobs+1 {
		t.Fatalf("в истории осталось %d jobs, ожидали около %d", kept, maxFinishedJobs)
	}
	// Последний запуск остаётся доступным.
	if _, err := store.Status(ids[len(ids)-1], 0, 500, ""); err != nil {
		t.Fatalf("последний job потерялся: %v", err)
	}
}
