package materialize

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type cacheNamespaceObserver struct {
	slog.Handler
	mutate func(string)
}

func (h cacheNamespaceObserver) Handle(ctx context.Context, r slog.Record) error {
	phase, path := "", ""
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "phase" {
			phase = a.Value.String()
		}
		if a.Key == "path" {
			path = a.Value.String()
		}
		return true
	})
	if phase == "cache-namespace" {
		h.mutate(path)
	}
	return h.Handler.Handle(ctx, r)
}

// spec: materialization/cache-namespace-creation-refuses-retargeted-parent
func TestRunCacheNamespaceCreationRefusesRetargetedParent(t *testing.T) {
	home, repo, source, foreign := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("inert support payload"), 0o640); err != nil {
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
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "namespace-probe", Path: source, Resource: ir.NewSkill("namespace-probe", "Source control", "inert compiled body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	retained := filepath.Join(t.TempDir(), "original-parent")
	parent := ""
	changed := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(cacheNamespaceObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(path string) {
		if changed {
			return
		}
		changed = true
		parent = path
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, parent); err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := Run(t.Context(), repo, resolved)
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 1 || entries[0].Name() != "foreign.txt" {
		t.Fatalf("cache creation mutated foreign namespace: hit=%t entries=%v err=%v", changed, entries, err)
	}
	if err != nil || !changed || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("namespace retarget did not refuse: hit=%t report=%+v err=%v", changed, report, err)
	}
	if raw, err := os.ReadFile(foreignFile); err != nil || string(raw) != "foreign inert bytes" {
		t.Fatalf("foreign payload changed: %q %v", raw, err)
	}
	if after, err := os.Lstat(foreignFile); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign generation changed: %v", err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("refusal left attempt cache/stage members: %v %v", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "namespace-probe")); !os.IsNotExist(err) {
		t.Fatalf("refused native link installed: %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(retained, parent); err != nil {
		t.Fatal(err)
	}
	report, err = Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("restored original namespace failed: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "namespace-probe", "reference.txt")
	if raw, err := os.ReadFile(installed); err != nil || string(raw) != "inert support payload" {
		t.Fatalf("healthy actual native support absent: %q %v", raw, err)
	}
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("native support mode changed: %v", err)
	}
	if stages, err := filepath.Glob(filepath.Join(parent, ".support-staging-*")); err != nil || len(stages) != 0 {
		t.Fatalf("healthy producer staging remained: %v %v", stages, err)
	}
}
