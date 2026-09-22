package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestContextFillNotice(t *testing.T) {
	got := contextFillNotice(7000, 8192)
	if !strings.Contains(got, "7000") || !strings.Contains(got, "8192") {
		t.Fatalf("notice=%q", got)
	}
	unknown := contextFillNoticeUnknown(9000)
	if !strings.Contains(unknown, "9000") || !strings.Contains(unknown, "OLLAMA_CONTEXT_LENGTH") {
		t.Fatalf("unknown notice=%q", unknown)
	}
}

func TestContextPayload(t *testing.T) {
	var got struct {
		Used  int `json:"used"`
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal([]byte(contextPayload(1234, 8192)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Used != 1234 || got.Limit != 8192 {
		t.Fatalf("payload=%+v", got)
	}
}

func TestLocalContextNoticeOncePerRun(t *testing.T) {
	r := &Runner{ProviderID: "local:default", LocalNumCtx: 8192}
	if got := r.localContextNotice(&llm.Usage{PromptTokens: 1000}); got != "" {
		t.Fatalf("low fill must stay silent, got %q", got)
	}
	if got := r.localContextNotice(&llm.Usage{PromptTokens: 7000}); got == "" {
		t.Fatal("expected warning at 85% fill")
	}
	if got := r.localContextNotice(&llm.Usage{PromptTokens: 8000}); got != "" {
		t.Fatalf("warning must not repeat, got %q", got)
	}
	// Контекст не задан: молчим до 8k, дальше предупреждаем про OLLAMA_CONTEXT_LENGTH.
	unset := &Runner{ProviderID: "local:default"}
	if got := unset.localContextNotice(&llm.Usage{PromptTokens: 4000}); got != "" {
		t.Fatalf("unset numCtx below threshold must stay silent, got %q", got)
	}
	if got := unset.localContextNotice(&llm.Usage{PromptTokens: 9000}); !strings.Contains(got, "OLLAMA_CONTEXT_LENGTH") {
		t.Fatalf("expected unknown-context warning, got %q", got)
	}
	// Облачные провайдеры не трогаем.
	cloud := &Runner{ProviderID: "deepseek", LocalNumCtx: 8192}
	if got := cloud.localContextNotice(&llm.Usage{PromptTokens: 8000}); got != "" {
		t.Fatalf("cloud must stay silent, got %q", got)
	}
}

func TestEmptyAnswerNudgeMentionsThinking(t *testing.T) {
	plain := emptyAnswerNudge(0, false)
	if !strings.Contains(plain, "plain text") {
		t.Fatalf("nudge=%q", plain)
	}
	if strings.Contains(plain, "thinking block") {
		t.Fatal("plain nudge must not mention thinking")
	}
	think := emptyAnswerNudge(8192, true)
	if !strings.Contains(think, "thinking block") || !strings.Contains(think, "8192") {
		t.Fatalf("nudge=%q", think)
	}
}

func TestEmptyAnswerErrorDiagnostics(t *testing.T) {
	think := emptyAnswerError("stop", 0, true, 0)
	if !strings.Contains(think, "thinking-only") {
		t.Fatalf("err=%q", think)
	}
	overflow := emptyAnswerError("stop", 4096, false, 5000)
	if !strings.Contains(overflow, "обрезал историю") {
		t.Fatalf("err=%q", overflow)
	}
	plain := emptyAnswerError("stop", 0, false, 0)
	if !strings.Contains(plain, "finish=stop") || strings.Contains(plain, "обрезал историю") {
		t.Fatalf("err=%q", plain)
	}
}

func TestLocalModelNoticeText(t *testing.T) {
	got := localModelNoticeText("big:70b", "qwen2.5-coder:7b", "local-fast")
	for _, want := range []string{"big:70b", "qwen2.5-coder:7b", "local-fast", "Auto-models"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice=%q missing %q", got, want)
		}
	}
}

// Раньше любой ответ длиннее 80 символов после чтения файлов принимался за
// догадку: текст чистился (delta_clear) и модель просили «ответить коротко».
func TestLocalNudgeRequiresNoToolsDone(t *testing.T) {
	w := newStallWatch(0)
	if w.toolsDone() != 0 {
		t.Fatalf("toolsDone=%d want 0", w.toolsDone())
	}
	w.onToolDone()
	w.onToolDone()
	if w.toolsDone() != 2 {
		t.Fatalf("toolsDone=%d want 2", w.toolsDone())
	}
	var nilWatch *stallWatch
	if nilWatch.toolsDone() != 0 {
		t.Fatal("nil watch must report 0")
	}
}

func TestResolveModelLocalAutoNotice(t *testing.T) {
	var notices []string
	emit := func(ev Event) {
		if ev.Type == "notice" {
			notices = append(notices, ev.Content)
		}
	}
	r := &Runner{
		AutoModels:     true,
		ProviderID:     "local:default",
		UserText:       "прочитай файлы и опиши подробно",
		PreferredModel: "big-generic:70b",
		LocalModels: []LocalModelInfo{
			NewLocalModelInfo("big-generic:70b", []string{"tools"}),
			NewLocalModelInfo("qwen2.5-coder:7b", []string{"tools"}),
		},
	}
	got := r.resolveModel(0, emit)
	if got != "qwen2.5-coder:7b" {
		t.Fatalf("model=%q want smallest coder", got)
	}
	if len(notices) != 1 {
		t.Fatalf("notices=%v want exactly one", notices)
	}
	if !strings.Contains(notices[0], "big-generic:70b") || !strings.Contains(notices[0], "qwen2.5-coder:7b") {
		t.Fatalf("notice=%q", notices[0])
	}
	// Второй шаг молчит: предупреждение одно на прогон.
	_ = r.resolveModel(1, emit)
	if len(notices) != 1 {
		t.Fatalf("notice repeated: %v", notices)
	}
}

func TestResolveModelLocalPreferredKeptSilently(t *testing.T) {
	var notices []string
	emit := func(ev Event) {
		if ev.Type == "notice" {
			notices = append(notices, ev.Content)
		}
	}
	r := &Runner{
		AutoModels:     true,
		ProviderID:     "local:default",
		UserText:       "привет",
		PreferredModel: "qwen2.5-coder:14b",
		LocalModels: []LocalModelInfo{
			NewLocalModelInfo("qwen2.5-coder:7b", []string{"tools"}),
			NewLocalModelInfo("qwen2.5-coder:14b", []string{"tools"}),
		},
	}
	if got := r.resolveModel(0, emit); got != "qwen2.5-coder:14b" {
		t.Fatalf("model=%q want preferred", got)
	}
	if len(notices) != 0 {
		t.Fatalf("no notice expected, got %v", notices)
	}
}
