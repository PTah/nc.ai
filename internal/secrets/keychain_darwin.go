//go:build darwin

package secrets

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

type osBackend struct{}

func newOSBackend() backend { return osBackend{} }

func (osBackend) Get(id string) (string, error) {
	out, err := runSecurity(8*time.Second, "find-generic-password", "-s", Service, "-a", id, "-w")
	if err != nil {
		if isNotFound(err, out) {
			return "", ErrNotFound
		}
		return "", err
	}
	v := strings.TrimSpace(out)
	if v == "" {
		return "", ErrNotFound
	}
	return v, nil
}

func (osBackend) Set(id, secret string) error {
	_, err := runSecurity(8*time.Second, "add-generic-password", "-s", Service, "-a", id, "-w", secret, "-U")
	return err
}

func (osBackend) Delete(id string) error {
	out, err := runSecurity(8*time.Second, "delete-generic-password", "-s", Service, "-a", id)
	if err != nil && isNotFound(err, out) {
		return ErrNotFound
	}
	return err
}

func runSecurity(timeout time.Duration, args ...string) (string, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return msg, err
		}
		return out, err
	}
	return out, nil
}

func isNotFound(err error, out string) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error() + " " + out)
	return strings.Contains(s, "could not be found") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "item not found")
}
