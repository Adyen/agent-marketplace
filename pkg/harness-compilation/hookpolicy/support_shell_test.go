package hookpolicy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// spec: marketplace-api/mirrored-hook-support-shell-safety
func TestSupportBaseShellSafety(t *testing.T) {
	for _, base := range []string{"path with spaces", "$(touch marker)", "path'quote", "plain"} {
		for _, quoted := range []bool{false, true} {
			t.Run(base+map[bool]string{true: "/quoted", false: "/bare"}[quoted], func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, base)
				if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "hooks", "run.sh"), []byte("#!/bin/sh\nprintf safe"), 0o700); err != nil {
					t.Fatal(err)
				}
				command := "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh"
				if quoted {
					command = `"` + command + `"`
				}
				hook := ir.NewHook("guard", "", command, "command", nil, ir.SourceRef{})
				resolved := Resolve(hook, target.TargetFactory, ScopeMachine, dir)
				if resolved.Failed() {
					t.Fatalf("safe representable path refused: %v", resolved.Diagnostics)
				}
				run := exec.CommandContext(t.Context(), "/bin/sh", "-c", resolved.Command)
				run.Dir = root
				if output, err := run.CombinedOutput(); err != nil || string(output) != "safe" {
					t.Fatalf("command %q: %q, %v", resolved.Command, output, err)
				}
				if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
					t.Fatal("path text executed shell substitution")
				}
			})
		}
	}
}
