package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"notcursor.ai/app/internal/llm"
)

const (
	// progressToolEvery is how many executed tools trigger a milestone update.
	progressToolEvery = 3
	// progressHeartbeat is the keep-alive cadence, so a run that lasts minutes
	// keeps telling the user which stage it is on.
	progressHeartbeat = 30 * time.Second
	// progressMinGap throttles milestone updates when tools come in bursts.
	progressMinGap = 25 * time.Second
)

// ProgressInfo is the payload of Event{Type: "progress"}: a compact snapshot of
// a running turn, so the UI can show the stage without exposing reasoning.
type ProgressInfo struct {
	Phase   string `json:"phase"` // start|step|heartbeat
	Elapsed string `json:"elapsed"`
	Seconds int    `json:"seconds"`
	Step    int    `json:"step"`
	Total   int    `json:"total"`
	Tools   int    `json:"tools"`
	Done    string `json:"done,omitempty"`
	Current string `json:"current,omitempty"`
	// Action is the same step written as a human phrase ("Читаю app.go"),
	// so the UI can show what the agent is doing without tool jargon.
	Action string `json:"action,omitempty"`
	Task   string `json:"task,omitempty"`
	Text   string `json:"text,omitempty"`
}

// runProgress accumulates what the agent already did during one RunMessage call.
type runProgress struct {
	mu        sync.Mutex
	start     time.Time
	step      int
	total     int
	tools     int
	sinceEmit int
	lastEmit  time.Time
	cats      map[string]int
	order     []string
	current   string
	act       string
	task      string
	stop      chan struct{}
	stopped   bool
}

func newRunProgress(total int, userText string) *runProgress {
	return &runProgress{
		start:    time.Now(),
		total:    total,
		lastEmit: time.Now(),
		cats:     map[string]int{},
		task:     firstLine(userText, 90),
		stop:     make(chan struct{}),
	}
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

func fmtElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func (p *runProgress) beginStep(step int) {
	p.mu.Lock()
	p.step = step
	p.mu.Unlock()
}

// setCurrent remembers the tool the runner is about to execute.
func (p *runProgress) setCurrent(name, argsJSON string) {
	hint := toolArgHint(name, argsJSON)
	act := toolAction(name, argsJSON)
	p.mu.Lock()
	if hint == "" {
		p.current = name
	} else {
		p.current = name + " " + hint
	}
	p.act = act
	p.mu.Unlock()
}

// noteTools counts one finished tool batch and reports whether a milestone
// update is due (enough tools or enough time since the last one).
func (p *runProgress) noteTools(calls []llm.ToolCall) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, call := range calls {
		label := toolCategory(call.Function.Name)
		if _, seen := p.cats[label]; !seen {
			p.order = append(p.order, label)
		}
		p.cats[label]++
		p.tools++
		p.sinceEmit++
	}
	if p.stopped {
		return "", false
	}
	if p.sinceEmit < progressToolEvery && time.Since(p.lastEmit) < progressMinGap {
		return "", false
	}
	return p.payloadLocked("step"), true
}

// heartbeat keeps the UI informed while a model call or a long tool runs.
func (p *runProgress) heartbeat(ctx context.Context, emit EmitFunc) {
	t := time.NewTicker(progressHeartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			stopped := p.stopped
			p.mu.Unlock()
			if stopped || ctx.Err() != nil {
				return
			}
			emit(Event{Type: "progress", Content: p.payload("heartbeat")})
		}
	}
}

func (p *runProgress) stopRun() {
	p.mu.Lock()
	stopped := p.stopped
	p.stopped = true
	p.mu.Unlock()
	if !stopped {
		close(p.stop)
	}
}

func (p *runProgress) payload(phase string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return ""
	}
	return p.payloadLocked(phase)
}

func (p *runProgress) payloadLocked(phase string) string {
	p.lastEmit = time.Now()
	p.sinceEmit = 0
	el := time.Since(p.start)
	info := ProgressInfo{
		Phase:   phase,
		Elapsed: fmtElapsed(el),
		Seconds: int(el.Seconds()),
		Step:    p.step,
		Total:   p.total,
		Tools:   p.tools,
		Done:    p.doneLocked(),
		Current: p.current,
		Action:  p.act,
		Task:    p.task,
		Text:    p.textLocked(phase, el),
	}
	b, err := json.Marshal(info)
	if err != nil {
		return ""
	}
	return string(b)
}

func (p *runProgress) textLocked(phase string, el time.Duration) string {
	var b strings.Builder
	switch phase {
	case "start":
		b.WriteString("Старт: ")
		if p.task == "" {
			b.WriteString("новая задача")
		} else {
			b.WriteString(p.task)
		}
		if p.total > 0 {
			fmt.Fprintf(&b, " · шаг 0/%d", p.total)
		} else {
			b.WriteString(" · без лимита шагов")
		}
	case "heartbeat":
		fmt.Fprintf(
			&b, "Работа идёт: %s · %s · вызовов инструментов %d",
			fmtElapsed(el), p.stepLabel(), p.tools,
		)
	default:
		fmt.Fprintf(
			&b, "Этап: %s · прошло %s · вызовов инструментов %d",
			p.stepLabel(), fmtElapsed(el), p.tools,
		)
	}
	if s := p.doneLocked(); s != "" {
		b.WriteString("\nСделано: " + s)
	}
	cur := p.act
	if cur == "" {
		cur = p.current
	}
	if cur != "" {
		b.WriteString("\nСейчас: " + cur)
	}
	return b.String()
}

// stepLabel is "шаг 7/120", or just "шаг 7" when the run has no step cap.
func (p *runProgress) stepLabel() string {
	if p.total > 0 {
		return fmt.Sprintf("шаг %d/%d", p.step, p.total)
	}
	return fmt.Sprintf("шаг %d (без лимита)", p.step)
}

func (p *runProgress) doneLocked() string {
	if len(p.order) == 0 {
		return ""
	}
	parts := make([]string, 0, len(p.order))
	for _, label := range p.order {
		parts = append(parts, fmt.Sprintf("%s ×%d", label, p.cats[label]))
	}
	return strings.Join(parts, ", ")
}

func toolCategory(name string) string {
	switch {
	case strings.HasPrefix(name, "git_"):
		return "git"
	case strings.HasPrefix(name, "ssh_"):
		return "ssh"
	}
	switch name {
	case "read_file", "grep", "glob", "list_dir", "find_files", "read_lints":
		return "чтение"
	case "apply_patch", "write_file", "delete_file", "move_file":
		return "правки"
	case "run_terminal", "command_status":
		return "команды"
	case "web_search", "fetch_url":
		return "web"
	case "todo_write":
		return "план"
	case "ask_user":
		return "вопрос"
	default:
		return "инструменты"
	}
}

// toolArgHint extracts a short human hint from tool arguments (path, query…).
func toolArgHint(name, argsJSON string) string {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return firstLine(argsJSON, 60)
	}
	for _, key := range []string{"path", "file", "rel", "abs", "query", "pattern", "command", "url", "name", "title"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return firstLine(s, 60)
			}
		}
	}
	if v, ok := raw["paths"]; ok {
		if list, ok := v.([]any); ok && len(list) > 0 {
			if s, ok := list[0].(string); ok {
				return firstLine(s, 60)
			}
		}
	}
	return ""
}

// toolAction describes the current step as a short human phrase ("Читаю app.go",
// "Ищу «EdgeSwitch»") so the live status line does not have to show tool names
// and raw arguments.
func toolAction(name, argsJSON string) string {
	hint := toolArgHint(name, argsJSON)
	switch {
	case strings.HasPrefix(name, "git_"):
		return gitAction(name)
	case strings.HasPrefix(name, "ssh_"):
		if name == "ssh_keygen" {
			return withTarget("Создаю SSH-ключ ", hint, "Создаю SSH-ключ")
		}
		if hint != "" {
			return "SSH: " + hint
		}
		return "Подключаюсь по SSH"
	}
	switch name {
	case "read_file":
		return withTarget("Читаю ", baseName(hint), "Читаю файл")
	case "write_file":
		return withTarget("Пишу ", baseName(hint), "Пишу файл")
	case "apply_patch":
		return withTarget("Правлю ", baseName(hint), "Правлю файлы")
	case "delete_file":
		return withTarget("Удаляю ", baseName(hint), "Удаляю файл")
	case "move_file":
		return withTarget("Перемещаю ", baseName(hint), "Перемещаю файл")
	case "list_dir":
		return withTarget("Смотрю каталог ", hint, "Смотрю каталог")
	case "glob":
		return withTarget("Ищу по маске ", hint, "Ищу по маске")
	case "find_files":
		return withTarget("Ищу файл ", hint, "Ищу файл")
	case "grep":
		return withTarget("Ищу ", quote(hint), "Ищу по содержимому")
	case "read_lints":
		return withTarget("Читаю диагностику ", baseName(hint), "Читаю диагностику")
	case "run_terminal":
		return withTarget("Запускаю: ", hint, "Запускаю команду")
	case "command_status":
		return withTarget("Проверяю фоновую задачу ", hint, "Проверяю фоновую задачу")
	case "get_env_info":
		return "Смотрю окружение"
	case "web_search":
		return withTarget("Ищу в сети: ", quote(hint), "Ищу в сети")
	case "fetch_url":
		return withTarget("Открываю ", hint, "Открываю страницу")
	case "todo_write":
		return "Обновляю план"
	case "ask_user":
		return "Спрашиваю вас"
	}
	return "Вызываю " + name
}

func gitAction(name string) string {
	switch name {
	case "git_status":
		return "Смотрю git status"
	case "git_diff":
		return "Смотрю git diff"
	case "git_log":
		return "Смотрю историю коммитов"
	case "git_commit":
		return "Коммичу изменения"
	case "git_push":
		return "Пушу в удалённый репозиторий"
	}
	return "git: " + strings.TrimPrefix(name, "git_")
}

func withTarget(prefix, target, fallback string) string {
	if strings.TrimSpace(target) == "" {
		return fallback
	}
	return prefix + target
}

func quote(s string) string {
	if s == "" {
		return ""
	}
	return "«" + s + "»"
}

// baseName keeps the file name only: the status line has no room for a full path.
func baseName(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return firstLine(p, 40)
}
