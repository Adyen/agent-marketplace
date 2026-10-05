package materialize

import (
	"bytes"
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

func TestSupportCachePruneMissingCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	if err := pruneSkillCacheSupportFiles(path, nil); err != nil {
		t.Fatalf("missing cache changed no-op contract: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("missing cache was recreated: %v", err)
	}
}

type supportPruneObserver struct {
	slog.Handler
	mutate func(string)
}

func (h supportPruneObserver) Handle(ctx context.Context, record slog.Record) error {
	phase, path := "", ""
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "phase" {
			phase = attr.Value.String()
		}
		if attr.Key == "cache_dir" {
			path = attr.Value.String()
		}
		return true
	})
	if phase == "support-prune" {
		h.mutate(path)
	}
	return h.Handler.Handle(ctx, record)
}

// spec: materialization/support-cache-prune-refuses-retargeted-generation
func TestRunSupportCachePruneRefusesRetargetedGeneration(t *testing.T) {
	home, repo, source, foreign := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("inert current support"), 0o640); err != nil {
		t.Fatal(err)
	}
	foreignPayload := filepath.Join(foreign, "stale.txt")
	if err := os.WriteFile(foreignPayload, []byte("foreign inert support"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("foreign inert compiled"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(foreignPayload)
	if err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "cache-probe", Path: source, Resource: ir.NewSkill("cache-probe", "Support control", "inert compiled body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	retained := filepath.Join(t.TempDir(), "original-cache")
	changed := false
	cacheDir := ""
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(supportPruneObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(path string) {
		if changed {
			return
		}
		changed = true
		cacheDir = path
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, path); err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || !changed || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("cache retarget did not refuse: changed=%t report=%+v err=%v", changed, report, err)
	}
	if raw, err := os.ReadFile(foreignPayload); err != nil || string(raw) != "foreign inert support" {
		t.Fatalf("prune consumed foreign support: %q %v", raw, err)
	}
	if after, err := os.Lstat(foreignPayload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("prune changed foreign generation: %v", err)
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 2 {
		t.Fatalf("refusal wrote/staged foreign cache members: %v %v", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "cache-probe")); !os.IsNotExist(err) {
		t.Fatalf("refused cache was installed: %v", err)
	}
	if residue, err := filepath.Glob(filepath.Join(filepath.Dir(cacheDir), ".support-staging-*")); err != nil || len(residue) != 0 {
		t.Fatalf("refusal left support stage: %v %v", residue, err)
	}
	if err := os.Remove(cacheDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, cacheDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "obsolete.txt"), []byte("inert previous support"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("restored cache did not permit actual install: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "cache-probe")
	if raw, err := os.ReadFile(filepath.Join(installed, "reference.txt")); err != nil || string(raw) != "inert current support" {
		t.Fatalf("healthy native install did not publish actual support: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(filepath.Join(installed, "SKILL.md")); err != nil || !bytes.Contains(raw, []byte("inert compiled body")) {
		t.Fatalf("healthy compiled skill absent: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(installed, "obsolete.txt")); !os.IsNotExist(err) {
		t.Fatalf("healthy reconciliation did not prune previous support: %v", err)
	}
	if info, err := os.Stat(filepath.Join(installed, "reference.txt")); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("healthy support mode changed: %v", err)
	}
}
