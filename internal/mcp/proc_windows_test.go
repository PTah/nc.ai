// Created by nc.ai 1.2.13 on Papasha 2026-11-14 22:12:31
//go:build windows

// Created by nc.ai 1.2.13 on Papasha 2026-11-14 22:31:00
package mcp

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const treeHelperEnv = "NC_AI_MCP_TREE_HELPER"

// TestTreeHelperProcess is not a test: it is the child/grandchild of
// TestTreeGuardKillsGrandchildren, started from the same test binary.
func TestTreeHelperProcess(t *testing.T) {
	mode := os.Getenv(treeHelperEnv)
	if mode == "" {
		t.Skip("helper process for TestTreeGuardKillsGrandchildren")
	}
	if mode == "child" {
		gc := exec.Command(os.Args[0], "-test.run=TestTreeHelperProcess")
		gc.Env = append(os.Environ(), treeHelperEnv+"=grandchild")
		if err := gc.Start(); err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.WriteString("GRANDCHILD " + strconv.Itoa(gc.Process.Pid) + "\n")
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	}
	time.Sleep(2 * time.Minute)
	os.Exit(0)
}

// TestTreeGuardKillsGrandchildren is the regression test for the MCP process
// leak: killing only the direct child (npx-cli / cmd.exe) left the real server
// process behind. The job object must take the whole tree down.
func TestTreeGuardKillsGrandchildren(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestTreeHelperProcess")
	cmd.Env = append(os.Environ(), treeHelperEnv+"=child")
	guard, err := newTreeGuard(cmd)
	if err != nil {
		t.Fatalf("newTreeGuard: %v", err)
	}
	defer guard.close()

	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := guard.adopt(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatalf("adopt: %v", err)
	}

	pid := 0
	scanner := bufio.NewScanner(out)
	deadline := time.Now().Add(30 * time.Second)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "GRANDCHILD ") {
			pid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "GRANDCHILD")))
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	if pid <= 0 {
		_ = cmd.Process.Kill()
		t.Fatal("child did not report its grandchild pid")
	}
	if !processInSnapshot(uint32(pid)) {
		t.Fatalf("grandchild %d is not running — test setup is broken", pid)
	}

	// Closing the job (KILL_ON_JOB_CLOSE) must kill the grandchild too.
	guard.close()
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()

	gone := waitProcessGone(uint32(pid), 15*time.Second)
	if !gone {
		// Leave no stray helper behind on failure.
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		t.Fatalf("grandchild %d survived the job close (process leak)", pid)
	}
}

// processInSnapshot reports whether pid is listed as an active process.
func processInSnapshot(pid uint32) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return false
	}
	for {
		if pe.ProcessID == pid {
			return true
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			return false
		}
	}
}

// waitProcessGone polls the process list until pid disappears or the timeout
// expires.
func waitProcessGone(pid uint32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processInSnapshot(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !processInSnapshot(pid)
}
