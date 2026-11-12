// Created by nc.ai 1.1.18 on new-admin-pc 2026-11-12 10:28:58
package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/tools"
	"notcursor.ai/app/internal/workspace"
)

// blockingProvider висит внутри ChatCompletion, пока тест не отпустит: так
// видно, сколько подагентов реально бегут одновременно.
type blockingProvider struct {
	release     <-chan struct{}
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	calls       int
}

func (b *blockingProvider) Name() string { return "blocking" }

func (b *blockingProvider) ChatCompletion(ctx context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	b.mu.Lock()
	b.inFlight++
	b.calls++
	if b.inFlight > b.maxInFlight {
		b.maxInFlight = b.inFlight
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.inFlight--
		b.mu.Unlock()
	}()
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{
		Message:      llm.Message{Role: "assistant", Content: "готово"},
		FinishReason: "stop",
	}}}, nil
}

func (b *blockingProvider) peak() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxInFlight
}

func taskCall(id string) llm.ToolCall {
	return llm.ToolCall{
		ID: id, Type: "function",
		Function: llm.FunctionCall{Name: "task", Arguments: `{"description":"d","prompt":"p"}`},
	}
}

// Два task-вызова при SubagentsParallel=2 должны идти одновременно, а не
// выстраиваться в очередь (это и есть «параллельные подагенты»).
func TestSubagentsRunSimultaneously(t *testing.T) {
	release := make(chan struct{})
	prov := &blockingProvider{release: release}
	r := &Runner{
		Provider:           prov,
		Tools:              tools.NewRegistry(workspace.NewManager(), t.TempDir()),
		SubagentsEnabled:   true,
		SubagentsParallel:  2,
		SubagentsMaxPerRun: 3,
		SubagentsMaxSteps:  2,
		RetryBackoff:       time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outs := make([]llm.Message, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i] = r.runTask(ctx, taskCall(fmt.Sprintf("t%d", i)), func(Event) {})
		}(i)
	}
	// Ждём, пока оба подагента окажутся внутри провайдера.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && prov.peak() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	if got := prov.peak(); got != 2 {
		t.Fatalf("одновременно бежало %d подагентов, want 2; результаты: %q | %q", got, outs[0].Content, outs[1].Content)
	}
	for i, m := range outs {
		if m.Role != "tool" || m.ToolCallID == "" {
			t.Fatalf("результат %d не tool-сообщение: %+v", i, m)
		}
	}
}

// При SubagentsParallel=1 (по умолчанию) подагенты идут строго по очереди.
func TestSubagentsSerialByDefault(t *testing.T) {
	release := make(chan struct{})
	prov := &blockingProvider{release: release}
	r := &Runner{
		Provider:           prov,
		Tools:              tools.NewRegistry(workspace.NewManager(), t.TempDir()),
		SubagentsEnabled:   true,
		SubagentsMaxPerRun: 3,
		SubagentsMaxSteps:  2,
		RetryBackoff:       time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.runTask(ctx, taskCall(fmt.Sprintf("s%d", i)), func(Event) {})
		}(i)
	}
	// Пока висит первый, второй не должен войти в провайдера.
	time.Sleep(150 * time.Millisecond)
	if got := prov.peak(); got != 1 {
		close(release)
		wg.Wait()
		t.Fatalf("без параллельности одновременно бежало %d", got)
	}
	close(release)
	wg.Wait()
	if got := prov.peak(); got != 1 {
		t.Fatalf("peak=%d, want 1", got)
	}
}
