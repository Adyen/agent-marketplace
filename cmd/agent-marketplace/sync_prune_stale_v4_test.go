package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestSyncPruneStaleV4DirectSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, configPath := pruneFixture(t, "v4-direct-prune.git", func(origin string) string {
		return "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  main: {git: file://" + origin + ", ref: main}\nresources:\n  main: [skill/keeper, skill/gone]\n"
	})

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("sync --prune-stale: %v\n%s", err, output)
	}
	if !strings.Contains(output, `pruned stale skill "gone"`) {
		t.Fatalf("expected prune line for gone, got:\n%s", output)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(configData)
	if !strings.Contains(text, "skill/keeper") || strings.Contains(text, "skill/gone") {
		t.Fatalf("expected keeper retained and gone removed:\n%s", text)
	}
	if strings.Contains(text, "exclude") || strings.Contains(text, "registries:") {
		t.Fatalf("prune wrote an exclusion or legacy key:\n%s", text)
	}
	assertFileExists(t, filepath.Join(repoRoot, ".factory", "skills", "keeper", "SKILL.md"))
	lockData, err := lockfile.ReadBytes(filepath.Join(repoRoot, ".agents", "lock.yaml"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock, err := lockfile.Decode(lockData)
	if err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	if lock.APIVersion != lockfile.APIVersionV2 {
		t.Fatalf("lock apiVersion = %q, want v2", lock.APIVersion)
	}
}

func TestSyncPruneStaleV4MixedBatch(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, configPath := pruneFixture(t, "v4-mixed-prune.git", func(origin string) string {
		return "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: resources.yaml\nsources:\n  main: {git: file://" + origin + ", ref: main}\nresources:\n  main: [skill/keeper, skill/gone-root]\n"
	})
	fragmentPath := filepath.Join(repoRoot, ".agents", "resources.yaml")
	fragmentData := "resources:\n  main: [skill/gone-frag]\n"
	writeConfig(t, fragmentPath, fragmentData)

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("sync --prune-stale: %v\n%s", err, output)
	}
	if !strings.Contains(output, `pruned stale skill "gone-root"`) {
		t.Fatalf("expected prune line for gone-root:\n%s", output)
	}
	if strings.Contains(output, `pruned stale skill "gone-frag"`) {
		t.Fatalf("claimed to prune include-owned selection:\n%s", output)
	}
	if !strings.Contains(output, "include-owned") {
		t.Fatalf("expected include-owned refusal:\n%s", output)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configData), "skill/keeper") || strings.Contains(string(configData), "gone-root") {
		t.Fatalf("root config after prune:\n%s", configData)
	}
	gotFragment, err := os.ReadFile(fragmentPath)
	if err != nil || string(gotFragment) != fragmentData {
		t.Fatalf("fragment changed:\n%s", gotFragment)
	}
}

func TestSyncPruneStaleV4IncludeOwnedSelectionRefuses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, configPath := pruneFixture(t, "v4-frag-prune.git", func(origin string) string {
		return "apiVersion: agent-marketplace/v4\nsources:\n  main: {git: file://" + origin + ", ref: main}\ninclude: fragments/skills.yaml\nresources:\n  main: [skill/keeper]\n"
	})
	fragmentPath := filepath.Join(repoRoot, ".agents", "fragments", "skills.yaml")
	fragmentData := "resources:\n  main: [skill/gone]\n"
	writeConfig(t, fragmentPath, fragmentData)
	rootData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("sync include-owned selection: %v\n%s", err, output)
	}
	assertFileExists(t, filepath.Join(repoRoot, ".factory", "skills", "keeper", "SKILL.md"))
	if strings.Contains(output, `pruned stale skill "gone"`) {
		t.Fatalf("claimed to prune an include-owned selection:\n%s", output)
	}
	if !strings.Contains(output, "include-owned") {
		t.Fatalf("expected include-owned refusal in output:\n%s", output)
	}
	gotRoot, err := os.ReadFile(configPath)
	if err != nil || string(gotRoot) != string(rootData) {
		t.Fatalf("root changed:\n%s", gotRoot)
	}
	gotFragment, err := os.ReadFile(fragmentPath)
	if err != nil || string(gotFragment) != fragmentData {
		t.Fatalf("fragment changed:\n%s", gotFragment)
	}
}

func TestPruneStaleSelectionsReportsPublicationFailure(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents", "config.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []*resolver.SelectedNotFoundError{
		{Kind: ir.KindSkill, Name: "gone", URL: "https://example.test/repo.git", Ref: "main"},
	}
	pruned, _, fatal := pruneStaleSelections(context.Background(), repoRoot, stale)
	if fatal == nil {
		t.Fatal("expected publication failure")
	}
	if len(pruned) != 0 {
		t.Fatalf("reported prunes after publication failure: %#v", pruned)
	}
}
