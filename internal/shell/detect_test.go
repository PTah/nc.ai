package shell

import (
	"runtime"
	"testing"
)

func TestDetectDefaultShell(t *testing.T) {
	got := DetectDefaultShell()
	if got == "" {
		t.Fatal("DetectDefaultShell empty")
	}
	t.Logf("detected shell: %s", got)
	if ResolveShell("") != got {
		t.Fatalf("ResolveShell(\"\") = %q, want %q", ResolveShell(""), got)
	}
	if ResolveShell("auto") != got {
		t.Fatalf("ResolveShell(auto) = %q, want %q", ResolveShell("auto"), got)
	}
	if ResolveShell("C:\\custom\\pwsh.exe") != "C:\\custom\\pwsh.exe" {
		t.Fatal("ResolveShell should keep explicit path")
	}
}

func TestInteractiveArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		args := InteractiveArgs(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`)
		if len(args) == 0 || args[0] != "-NoLogo" {
			t.Fatalf("powershell args = %#v", args)
		}
		return
	}
	args := InteractiveArgs("/bin/zsh")
	if len(args) != 1 || args[0] != "-il" {
		t.Fatalf("zsh args = %#v", args)
	}
}

func TestIsPowerShell(t *testing.T) {
	if !IsPowerShell("pwsh.exe") || !IsPowerShell("powershell") {
		t.Fatal("expected PowerShell detection")
	}
	if IsPowerShell("/bin/zsh") {
		t.Fatal("zsh is not PowerShell")
	}
}
