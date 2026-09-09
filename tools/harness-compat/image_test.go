package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestImageScriptsRejectUnsafeInputsBeforeInstallation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		env  []string
	}{
		{"prerelease", []string{"install.sh"}, []string{"HARNESS_TARGET=devin", "CLI_VERSION=1.2.3-beta"}},
		{"unknown-target", []string{"install.sh"}, []string{"HARNESS_TARGET=unknown", "CLI_VERSION=1.2.3"}},
		{"wrong-package", []string{"install.sh"}, []string{"HARNESS_TARGET=claude", "CLI_VERSION=1.2.3", "NPM_PACKAGE=untrusted"}},
		{"invalid-checksum", []string{"install.sh"}, []string{"HARNESS_TARGET=devin", "CLI_VERSION=1.2.3", "BUNDLE_SHA256=invalid"}},
		{"untrusted-image", []string{"build-image.sh", "devin", "untrusted.example/image:ci"}, nil},
		{"unexpected-option", []string{"build-image.sh", "devin", "ghcr.io/example/repo/harness-devin:ci", "--unknown"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			marker := filepath.Join(bin, "external-call")
			for _, tool := range []string{"npm", "curl", "docker", "go"} {
				if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\nprintf called > \"$CALL_MARKER\"\nexit 97\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", tc.args...)
			cmd.Env = append([]string{"PATH=" + bin + ":/usr/bin:/bin", "CALL_MARKER=" + marker}, tc.env...)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 && exit.ExitCode() != 2 {
				t.Fatalf("unsafe invocation was not rejected before external tools: %v\n%s", err, out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("unsafe arguments reached an external tool")
			}
			control := exec.Command(filepath.Join(bin, "npm"))
			control.Env = []string{"CALL_MARKER=" + marker}
			_ = control.Run()
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("external-call positive control failed")
			}
		})
	}
}
