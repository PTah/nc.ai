// Edited by nc.ai 1.2.13 on Papasha 2026-11-14 22:55:00
// Created by nc.ai 0.8.5.4 on new-admin-pc 2026-10-13 10:03:06
// Edited by nc.ai 1.2.12 on Papasha 2026-11-14 21:54:28
//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow  = 0x08000000
	createSuspended = 0x00000004
)

// configureProcess prevents a console window from popping up for child
// processes. NotCursor is a GUI-subsystem binary with no console of its own, so
// any console child (node.exe, or cmd.exe running a .cmd shim like npm.cmd)
// would otherwise get a brand-new cmd window. Setting CREATE_NO_WINDOW +
// HideWindow keeps MCP servers and installers in the background.
//
// It is used by package installers (runCmdOut); MCP stdio servers go through
// newTreeGuard, which also sets these flags.
func configureProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// treeGuard owns a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE: closing the handle (Close) terminates the
// whole child tree. The process is started suspended, attached to the job, then
// resumed, so no grandchild can slip out of the job between CreateProcess and
// AssignProcessToJobObject. Without this, Session.Close killed only the direct
// child (npx-cli / cmd.exe) and left the real node/python server and its
// conhost orphaned — the classic MCP process leak.
type treeGuard struct {
	job windows.Handle
}

func newTreeGuard(cmd *exec.Cmd) (*treeGuard, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow | createSuspended
	return &treeGuard{job: job}, nil
}

// adopt attaches the just-started (suspended) process to the job and resumes its
// threads. Must be called exactly once, right after cmd.Start.
func (g *treeGuard) adopt(pid int) error {
	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(g.job, h); err != nil {
		return err
	}
	return resumeProcessThreads(uint32(pid))
}

// close destroys the job; KILL_ON_JOB_CLOSE terminates every process still in it.
func (g *treeGuard) close() {
	if g == nil || g.job == 0 {
		return
	}
	_ = windows.CloseHandle(g.job)
	g.job = 0
}

// resumeProcessThreads resumes every thread owned by pid. A suspended process
// starts with a single primary thread, but there is no harm in resuming all of
// them if the process already created more.
func resumeProcessThreads(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	if err := windows.Thread32First(snap, &te); err != nil {
		return err
	}
	for {
		if te.OwnerProcessID == pid {
			th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
			if err == nil {
				_, _ = windows.ResumeThread(th)
				_ = windows.CloseHandle(th)
			}
		}
		if err := windows.Thread32Next(snap, &te); err != nil {
			break
		}
	}
	return nil
}
