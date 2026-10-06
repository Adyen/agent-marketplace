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

func TestCopySupportFilesEmptySourceKeepsDestinationAbsent(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("compiled exclusion"), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "absent")
	if err := copySupportFiles(source, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("empty support created destination: %v", err)
	}
}

type supportOutputObserver struct {
	slog.Handler
	mutate func(string)
}

func (h supportOutputObserver) Handle(ctx context.Context, record slog.Record) error {
	phase, path := "", ""
	record.Attrs(func(a slog.Attr) bool {
		if a.Key == "phase" {
			phase = a.Value.String()
		}
		if a.Key == "path" {
			path = a.Value.String()
		}
		return true
	})
	if phase == "support-output" {
		h.mutate(path)
	}
	return h.Handler.Handle(ctx, record)
}

// spec: materialization/support-output-refuses-retarget-before-directory-and-file-writes
func TestRunSupportOutputRefusesRetargetBeforeDirectoryAndFileWrites(t *testing.T) {
	for _, rel := range []string{"reference.txt", "scripts/reference.txt"} {
		t.Run(rel, func(t *testing.T) { testSupportOutputRetarget(t, rel) })
	}
}

func testSupportOutputRetarget(t *testing.T, rel string) {
	home, repo, source, foreign := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(source, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, rel), []byte("inert scoped support"), 0o640); err != nil {
		t.Fatal(err)
	}
	foreignFile := filepath.Join(foreign, "foreign.txt")
	if err := os.WriteFile(foreignFile, []byte("foreign inert bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(foreignFile)
	if err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "output-probe", Path: source, Resource: ir.NewSkill("output-probe", "Support control", "compiled inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	retained := filepath.Join(t.TempDir(), "original-output")
	output := ""
	changed := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(supportOutputObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(path string) {
		if changed || !strings.HasPrefix(filepath.Base(path), ".support-staging-") {
			return
		}
		changed = true
		output = path
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, path); err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := Run(t.Context(), repo, resolved)
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 1 || entries[0].Name() != "foreign.txt" {
		t.Fatalf("support copy mutated foreign directory: hit=%t entries=%v err=%v", changed, entries, err)
	}
	if err != nil || !changed || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("retargeted support output did not refuse: hit=%t report=%+v err=%v", changed, report, err)
	}
	if raw, err := os.ReadFile(foreignFile); err != nil || string(raw) != "foreign inert bytes" {
		t.Fatalf("foreign payload changed: %q %v", raw, err)
	}
	if after, err := os.Lstat(foreignFile); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign generation changed: %v", err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("refused attempt wrote into original output: %v %v", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "output-probe")); !os.IsNotExist(err) {
		t.Fatalf("refused native skill installed: %v", err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, output); err != nil {
		t.Fatal(err)
	}
	report, err = Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("healthy native install failed: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "output-probe", rel)
	if raw, err := os.ReadFile(installed); err != nil || string(raw) != "inert scoped support" {
		t.Fatalf("actual native support missing: %q %v", raw, err)
	}
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("native support mode changed: %v", err)
	}
	stages, err := filepath.Glob(filepath.Join(filepath.Dir(output), ".support-staging-*"))
	if err != nil || len(stages) != 1 || stages[0] != output {
		t.Fatalf("healthy attempt left unexpected stage: %v %v", stages, err)
	}
}
