package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sleepCommand — долгая команда для проверки лимита, в синтаксисе текущей ОС.
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

func TestRunTimeoutReturnsAndHints(t *testing.T) {
	start := time.Now()
	_, err := Run(context.Background(), sleepCommand(30), "", "", time.Second)
	if err == nil {
		t.Fatal("ожидали таймаут")
	}
	if !IsTimeout(err) {
		t.Fatalf("IsTimeout=false, err=%v", err)
	}
	// Дерево процессов гасим сразу: выход не должен ждать саму команду.
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("таймаут сработал слишком поздно: %s", elapsed)
	}
	if !strings.Contains(err.Error(), "is_background=true") {
		t.Errorf("в ошибке нет подсказки про фон: %v", err)
	}
	if !strings.Contains(err.Error(), "1 сек") {
		t.Errorf("в ошибке нет лимита: %v", err)
	}
}

func TestRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	_, err := Run(ctx, sleepCommand(30), "", "", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "отменена") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunNormalCommand(t *testing.T) {
	res, err := Run(context.Background(), "echo hello", "", "", 10*time.Second)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("exit=%d stdout=%q", res.ExitCode, res.Stdout)
	}
}
