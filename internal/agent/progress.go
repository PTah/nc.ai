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
	Task    string `json:"task,omitempty"`
	Text    string `json:"text,omitempty"`
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
	p.mu.Lock()
	if hint == "" {
		p.current = name
	} else {
		p.current = name + " " + hint
	}
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
		fmt.Fprintf(&b, " · шаг 0/%d", p.total)
	case "heartbeat":
		fmt.Fprintf(
			&b, "Работа идёт: %s · шаг %d/%d · вызовов инструментов %d",
			fmtElapsed(el), p.step, p.total, p.tools,
		)
	default:
		fmt.Fprintf(
			&b, "Этап: шаг %d/%d · прошло %s · вызовов инструментов %d",
			p.step, p.total, fmtElapsed(el), p.tools,
		)
	}
	if s := p.doneLocked(); s != "" {
		b.WriteString("\nСделано: " + s)
	}
	if p.current != "" {
		b.WriteString("\nСейчас: " + p.current)
	}
	return b.String()
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
