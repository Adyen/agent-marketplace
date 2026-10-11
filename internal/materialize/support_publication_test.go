package materialize

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type supportPublicationObserver struct {
	slog.Handler
	mutate func(string, string)
}

func (h supportPublicationObserver) Handle(ctx context.Context, record slog.Record) error {
	phase, path, stage := "", "", ""
	record.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "phase":
			phase = a.Value.String()
		case "path":
			path = a.Value.String()
		case "staged":
			stage = a.Value.String()
		}
		return true
	})
	if phase == "file-publication" {
		h.mutate(path, stage)
	}
	return h.Handler.Handle(ctx, record)
}

// spec: materialization/support-publication-refuses-replaced-parent-and-stage
func TestRunSupportPublicationRefusesReplacedParentAndStage(t *testing.T) {
	home, repo, source, foreign := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.Mkdir(filepath.Join(source, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "scripts", "reference.txt"), []byte("inert support control"), 0o640); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "publication-probe", Path: source, Resource: ir.NewSkill("publication-probe", "Support control", "compiled inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	retained := filepath.Join(t.TempDir(), "retained-parent")
	changed := false
	parent, stageName := "", ""
	var foreignInfo os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(supportPublicationObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(path, stage string) {
		if changed || filepath.Base(path) != "reference.txt" || strings.Contains(path, ".support-staging-") {
			return
		}
		changed = true
		parent = filepath.Dir(path)
		stageName = filepath.Base(stage)
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, parent); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(foreign, stageName), []byte("foreign inert staged bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		foreignInfo, err = os.Lstat(filepath.Join(foreign, stageName))
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || !changed || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("support publisher consumed replacement namespace: hit=%t report=%+v err=%v", changed, report, err)
	}
	foreignStage := filepath.Join(foreign, stageName)
	if raw, err := os.ReadFile(foreignStage); err != nil || string(raw) != "foreign inert staged bytes" {
		t.Fatalf("publisher renamed/removed foreign stage: %q %v", raw, err)
	}
	if after, err := os.Lstat(foreignStage); err != nil || !os.SameFile(foreignInfo, after) || foreignInfo.Mode() != after.Mode() || !foreignInfo.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign stage generation changed: %v", err)
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 1 {
		t.Fatalf("publication changed foreign members: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("refusal left original staging bytes: %v %v", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "publication-probe")); !os.IsNotExist(err) {
		t.Fatalf("refused native skill was published: %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, parent); err != nil {
		t.Fatal(err)
	}
	report, err = Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("restored parent failed real native publication: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "publication-probe", "scripts", "reference.txt")
	if raw, err := os.ReadFile(installed); err != nil || string(raw) != "inert support control" {
		t.Fatalf("healthy native support missing: %q %v", raw, err)
	}
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("native support mode changed: %v", err)
	}
}
