package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4ToggleFailureKeepsInvalidConfigBytesThroughStaging(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", t.TempDir())
	path := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("apiVersion: agent-marketplace/v4\nunknown_field: true\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	act := toggleAction{skillName: "alpha", kind: ir.KindSkill, scope: scopeGlobal, enable: true, registryURL: filepath.Join(t.TempDir(), "registry"), registryKind: config.V4SourceDirectory}
	workspace, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	reconciled := false
	msg, ok := runDependencyScopedToggle(context.Background(), workspace, act, func(ctx context.Context, ws pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
		return pipeline.MutateConfigWithDependencies(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mutation, func(context.Context, string, string, bool, []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
			reconciled = true
			return pipeline.DependencyLockView{}, nil
		})
	})().(configWrittenMsg)
	if !ok || msg.err == nil {
		t.Fatalf("failure message = %#v", msg)
	}
	var validation *config.V4ValidationError
	if !errors.As(msg.err, &validation) || validation.Code != "v4.unknown-field" {
		t.Fatalf("error = %T %v", msg.err, msg.err)
	}
	if reconciled {
		t.Fatal("invalid v4 edit reached dependency reconciliation")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) || !strings.Contains(string(got), "unknown_field") {
		t.Fatalf("invalid config changed:\n%s", got)
	}
	assertNoConfigStaging(t)
}
