package materialize

import (
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

// spec: materialization/support-producer-cleanup-preserves-replaced-stage-directory
func TestRunSupportProducerCleanupPreservesReplacementDirectory(t *testing.T) {
	home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("inert support payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "cleanup-probe", Path: source, Resource: ir.NewSkill("cleanup-probe", "Support control", "compiled inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	retained := filepath.Join(t.TempDir(), "retained-producer")
	changed := false
	producer := ""
	var replacement os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(supportPublicationObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(path, stage string) {
		if changed || filepath.Base(path) != "reference.txt" || strings.Contains(path, ".support-staging-") {
			return
		}
		stages, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(path)), ".support-staging-*"))
		if err != nil || len(stages) != 1 {
			t.Fatalf("actual producer stage absent: %v %v", stages, err)
		}
		changed = true
		producer = stages[0]
		if err := os.Rename(producer, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(producer, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(producer, "foreign.txt"), []byte("foreign inert replacement"), 0o640); err != nil {
			t.Fatal(err)
		}
		replacement, err = os.Lstat(filepath.Join(producer, "foreign.txt"))
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := Run(t.Context(), repo, resolved)
	foreign := filepath.Join(producer, "foreign.txt")
	if raw, err := os.ReadFile(foreign); err != nil || string(raw) != "foreign inert replacement" {
		t.Fatalf("producer cleanup deleted replacement bytes: hit=%t raw=%q err=%v", changed, raw, err)
	}
	if err != nil || !changed || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("changed producer did not refuse native installation: hit=%t report=%+v err=%v", changed, report, err)
	}
	if after, err := os.Lstat(foreign); err != nil || !os.SameFile(replacement, after) || replacement.Mode() != after.Mode() || !replacement.ModTime().Equal(after.ModTime()) {
		t.Fatalf("producer cleanup changed foreign generation: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(retained, "reference.txt")); err != nil || string(raw) != "inert support payload" {
		t.Fatalf("cleanup changed retained original: %q %v", raw, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "cleanup-probe")); !os.IsNotExist(err) {
		t.Fatalf("cleanup refusal installed native link: %v", err)
	}
	report, err = Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("healthy retry failed: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "cleanup-probe", "reference.txt")
	if raw, err := os.ReadFile(installed); err != nil || string(raw) != "inert support payload" {
		t.Fatalf("healthy native support absent: %q %v", raw, err)
	}
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("native mode changed: %v", err)
	}
	if stages, err := filepath.Glob(filepath.Join(filepath.Dir(producer), ".support-staging-*")); err != nil || len(stages) != 1 || stages[0] != producer {
		t.Fatalf("healthy attempt did not clean its own stage: %v %v", stages, err)
	}
	if raw, err := os.ReadFile(foreign); err != nil || string(raw) != "foreign inert replacement" {
		t.Fatalf("healthy cleanup removed previous foreign replacement: %q %v", raw, err)
	}
}

func TestSupportStageCleanupNeverRemovesReplacementSwappedDuringCleanup(t *testing.T) {
	home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("inert support payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "cleanup-probe", Path: source, Resource: ir.NewSkill("cleanup-probe", "Support control", "compiled inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	retained := filepath.Join(t.TempDir(), "retained-producer")
	stagePath := ""
	supportStageCleanupHook = func(stage string) {
		if stagePath != "" {
			return
		}
		stagePath = stage
		if err := os.Rename(stage, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(stage, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "foreign.txt"), []byte("foreign inert replacement"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { supportStageCleanupHook = nil })
	report, err := Run(t.Context(), repo, resolved)
	if stagePath == "" {
		t.Fatal("support stage cleanup seam not reached")
	}
	if raw, readErr := os.ReadFile(filepath.Join(stagePath, "foreign.txt")); readErr != nil || string(raw) != "foreign inert replacement" {
		t.Fatalf("cleanup removed a replacement swapped in after its identity check: %q %v", raw, readErr)
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("replaced stage did not refuse: report=%+v err=%v", report, err)
	}
	if entries, readErr := os.ReadDir(retained); readErr != nil || len(entries) != 0 {
		t.Fatalf("admitted stage contents were not removed through its descriptor: %v %v", entries, readErr)
	}
}

func TestSupportOutputStaysWithinAdmittedCacheParent(t *testing.T) {
	home, repo, source, external := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("inert support payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "scope-probe", Path: source, Resource: ir.NewSkill("scope-probe", "Support control", "compiled inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	retained := filepath.Join(t.TempDir(), "retained-cache-parent")
	swapped := ""
	supportStageCopyHook = func(parent string) {
		if swapped != "" {
			return
		}
		swapped = parent
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, parent); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { supportStageCopyHook = nil })
	report, err := Run(t.Context(), repo, resolved)
	if swapped == "" {
		t.Fatal("support stage copy seam not reached")
	}
	if entries, readErr := os.ReadDir(external); readErr != nil || len(entries) != 0 {
		t.Fatalf("support output followed a swapped cache parent: %v %v", entries, readErr)
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("swapped cache parent did not refuse: report=%+v err=%v", report, err)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".factory", "skills", "scope-probe")); !os.IsNotExist(statErr) {
		t.Fatalf("refused support output installed a native link: %v", statErr)
	}
}
