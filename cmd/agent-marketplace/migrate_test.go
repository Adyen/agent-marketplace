package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: consumer-config-format/migration-command-and-flags-are-absent
func TestConsumerMigrationSurfaceIsAbsent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo := t.TempDir()
	withWorkingDir(t, repo)
	path := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte("apiVersion: agent-marketplace/v2\nregistries: []\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"migrate"}, {"migrate", "--to", "v2"}, {"migrate", "--to", "v4"}} {
		cmd := newRootCmd()
		cmd.SetArgs(args)
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("%v: expected unknown command, got %v", args, err)
		}
	}
	for _, args := range [][]string{{"--help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
		cmd := newRootCmd()
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(new(bytes.Buffer))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(out.String(), "migrate") {
			t.Fatalf("%v exposes migration: %s", args, &out)
		}
	}
	convert := newConvertCmd()
	if convert.Flags().Lookup("to") == nil {
		t.Fatal("harness convert --to was removed")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("unsupported root changed: %q, %v", after, err)
	}
}
