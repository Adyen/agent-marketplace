package materialize

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func TestSDKOutputComparisonRefusesFIFOWithoutBlocking(t *testing.T) {
	if output := os.Getenv("AM_TEST_SDK_COMPARISON_FIFO"); output != "" {
		result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("inert")}}}
		_, err := hcc.Materialize(result, output)
		if !errors.Is(err, fsboundary.ErrUnsafeFile) {
			t.Fatalf("SDK FIFO comparison did not refuse: %v", err)
		}
		return
	}
	output := t.TempDir()
	path := filepath.Join(output, "SKILL.md")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestSDKOutputComparisonRefusesFIFOWithoutBlocking$")
	cmd.Env = append(os.Environ(), "AM_TEST_SDK_COMPARISON_FIFO="+output)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bounded SDK FIFO refusal failed: %s %v", raw, err)
	}
	if after, err := os.Lstat(path); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("SDK FIFO generation changed: %v", err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 {
		t.Fatalf("SDK FIFO refusal staged output: %v %v", entries, err)
	}
}

// spec: materialization/sdk-output-comparison-refuses-multiply-linked-cache-file
func TestSDKOutputComparisonRefusesMultiplyLinkedFile(t *testing.T) {
	output, foreign := t.TempDir(), t.TempDir()
	result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("independently authored inert compiled payload"), Mode: 0o640}}}
	report, err := hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("initial SDK publication failed: %+v %v", report, err)
	}
	payload := filepath.Join(output, "SKILL.md")
	foreignFile := filepath.Join(foreign, "foreign.txt")
	if err := os.Link(payload, foreignFile); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreignFile)
	if err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("public SDK comparison admitted hardlink: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(foreignFile); err != nil || string(raw) != "independently authored inert compiled payload" {
		t.Fatalf("foreign linked payload changed: %q %v", raw, err)
	}
	if after, err := os.Stat(foreignFile); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign linked generation changed: %v", err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Fatalf("SDK refusal left staging: %v %v", entries, err)
	}
	if err := os.Remove(foreignFile); err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Skipped) != 1 || len(report.Written) != 0 {
		t.Fatalf("regular SDK no-op failed: %+v %v", report, err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("healthy no-op changed original generation: %v", err)
	}
	result.Artifacts[0].Bytes = []byte("independently authored updated inert payload")
	report, err = hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("healthy actual SDK update failed: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(payload); err != nil || string(raw) != "independently authored updated inert payload" {
		t.Fatalf("healthy SDK updated bytes absent: %q %v", raw, err)
	}
	if after, err := os.Stat(payload); err != nil || after.Mode().Perm() != 0o640 {
		t.Fatalf("SDK source mode changed: %v", err)
	}
}
