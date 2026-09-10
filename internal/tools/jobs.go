package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"notcursor.ai/app/internal/shell"
)

type jobStatus string

const (
	jobRunning jobStatus = "running"
	jobDone    jobStatus = "done"
)

type bgJob struct {
	ID      string
	Command string
	Cwd     string
	Started time.Time
	Ended   time.Time
	Status  jobStatus
	Exit    int
	Err     string
	Stdout  strings.Builder
	Stderr  strings.Builder
	mu      sync.Mutex
}

// JobStore tracks background run_terminal processes.
type JobStore struct {
	mu   sync.Mutex
	jobs map[string]*bgJob
}

func NewJobStore() *JobStore {
	return &JobStore{jobs: map[string]*bgJob{}}
}

func (s *JobStore) Start(command, cwd, shellPath string, timeout time.Duration) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is empty")
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	s.mu.Lock()
	if len(s.jobs) >= 8 {
		running := 0
		for _, j := range s.jobs {
			if j.Status == jobRunning {
				running++
			}
		}
		if running >= 8 {
			s.mu.Unlock()
			return "", fmt.Errorf("too many background jobs (max 8)")
		}
	}
	id := "job_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	j := &bgJob{ID: id, Command: command, Cwd: cwd, Started: time.Now(), Status: jobRunning}
	s.jobs[id] = j
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		res, err := shell.Run(ctx, command, cwd, shellPath, timeout)
		j.mu.Lock()
		j.Ended = time.Now()
		j.Status = jobDone
		if err != nil {
			j.Err = err.Error()
			j.Exit = -1
		} else {
			j.Exit = res.ExitCode
			j.Stdout.WriteString(res.Stdout)
			j.Stderr.WriteString(res.Stderr)
		}
		j.mu.Unlock()
	}()
	return id, nil
}

func (s *JobStore) Status(id string, waitSec, charCount int, priority string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("id is empty")
	}
	s.mu.Lock()
	j := s.jobs[id]
	s.mu.Unlock()
	if j == nil {
		return "", fmt.Errorf("unknown job %s", id)
	}
	if waitSec < 0 {
		waitSec = 0
	}
	if waitSec > 60 {
		waitSec = 60
	}
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for {
		j.mu.Lock()
		st := j.Status
		j.mu.Unlock()
		if st == jobDone || waitSec == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return formatJob(j, charCount, priority), nil
}

func formatJob(j *bgJob, charCount int, priority string) string {
	if charCount <= 0 {
		charCount = 4000
	}
	if charCount > 20000 {
		charCount = 20000
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "id=%s status=%s command=%s\n", j.ID, j.Status, j.Command)
	if j.Cwd != "" {
		fmt.Fprintf(&b, "cwd=%s\n", j.Cwd)
	}
	fmt.Fprintf(&b, "started=%s\n", j.Started.Format(time.RFC3339))
	if j.Status == jobDone {
		fmt.Fprintf(&b, "exit=%d elapsed=%s\n", j.Exit, j.Ended.Sub(j.Started).Truncate(time.Millisecond))
		if j.Err != "" {
			fmt.Fprintf(&b, "error=%s\n", j.Err)
		}
	}
	out := clipOutput(j.Stdout.String(), charCount, priority)
	err := clipOutput(j.Stderr.String(), charCount/2, priority)
	if out != "" {
		fmt.Fprintf(&b, "stdout:\n%s\n", out)
	}
	if err != "" {
		fmt.Fprintf(&b, "stderr:\n%s\n", err)
	}
	if j.Status == jobRunning {
		b.WriteString("(still running — call command_status again)\n")
	}
	return strings.TrimSpace(b.String())
}

func clipOutput(s string, n int, priority string) string {
	if len(s) <= n {
		return s
	}
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "top":
		return s[:n] + "\n…(truncated)"
	case "split":
		half := n / 2
		return s[:half] + "\n…(middle omitted)…\n" + s[len(s)-half:]
	default: // bottom
		return "…(truncated)\n" + s[len(s)-n:]
	}
}
