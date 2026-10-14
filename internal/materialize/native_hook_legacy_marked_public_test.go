package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

const (
	legacyV413GeminiMarkedGroup     = `{"matcher":"Bash","hooks":[{"type":"command","command":"echo d76-selected"}],"_managed_by":"agent-marketplace"}`
	legacyV413GeminiManualGroup     = `{"matcher":"^manual$","hooks":[{"type":"command","command":"echo d76-manual"}]}`
	legacyV413WindsurfMarkedHandler = `{"command":"echo d76-marked-windsurf","_managed_by":"agent-marketplace"}`
	legacyV413WindsurfManualHandler = `{"command":"echo d76-manual-windsurf"}`
)

func legacyV413Count(data []byte, needle string) int {
	return strings.Count(string(data), needle)
}

func legacyV413GeminiFile(root string) string {
	return filepath.Join(root, ".gemini", "settings.json")
}

func legacyV413GeminiSeed(group string) []byte {
	return []byte(`{
  "hooks" : {
    "BeforeTool" : [
      ` + legacyV413GeminiManualGroup + `,
      ` + group + `
    ]
  }
}
`)
}

func legacyV413RunGeminiTransition(t *testing.T, repo, home string, machine bool, file string, seed []byte) {
	t.Helper()
	writeGeminiMarkedSettings(t, file, seed)
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	seedGeminiMarkedBaseline(t, repo, home)
	selected := additiveSafetyProvider(t, hcc.TargetGemini, machine, "echo d76-selected", "")

	before, mode, mtime := fileSnapshot(t, file)
	preview, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes), WithDryRun())
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if len(preview.HookChanges) == 0 {
		t.Fatal("dry-run listed no hook changes for the v4.1.3 marked group")
	}
	assertFileUnchanged(t, file, before, mode, mtime)

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := legacyV413Count(after, "echo d76-selected"); got != 1 {
		t.Fatalf("selected hook copies = %d, want 1:\n%s", got, after)
	}
	if strings.Contains(string(after), "_managed_by") || strings.Contains(string(after), `"matcher":"Bash"`) {
		t.Fatalf("v4.1.3 marked group survived the transition:\n%s", after)
	}
	if !strings.Contains(string(after), legacyV413GeminiManualGroup) {
		t.Fatalf("handwritten group bytes changed:\n%s", after)
	}

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("deselecting sync: %v", err)
	}
	final, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if legacyV413Count(final, "echo d76-selected") != 0 {
		t.Fatalf("deselection left the managed token:\n%s", final)
	}
	if !strings.Contains(string(final), legacyV413GeminiManualGroup) {
		t.Fatalf("deselection removed the handwritten group:\n%s", final)
	}
}

func TestGeminiV413MarkedGroupRepoUpgradeAndDeselect(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	file := legacyV413GeminiFile(evalPath(t, repo))
	legacyV413RunGeminiTransition(t, repo, home, false, file, legacyV413GeminiSeed(legacyV413GeminiMarkedGroup))
}

func TestGeminiV413MarkedGroupUserUpgradeAndDeselect(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	file := legacyV413GeminiFile(evalPath(t, home))
	legacyV413RunGeminiTransition(t, repo, home, true, file, legacyV413GeminiSeed(legacyV413GeminiMarkedGroup))
}

func TestWindsurfV413CommandOnlyUserUpgradeAndDeselect(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	seedNativeHookJSONLock(t, filepath.Join(home, ".codeium", "windsurf"))
	file := filepath.Join(evalPath(t, home), ".codeium", "windsurf", "hooks.json")
	seed := []byte(`{"hooks":{"pre_run_command":[` + legacyV413WindsurfManualHandler + `,` + legacyV413WindsurfMarkedHandler + `]}}` + "\n")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, true, "echo d76-selected", "")

	before, mode, mtime := fileSnapshot(t, file)
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes), WithDryRun()); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	assertFileUnchanged(t, file, before, mode, mtime)

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := legacyV413Count(after, "echo d76-selected"); got != 1 {
		t.Fatalf("selected hook copies = %d, want 1:\n%s", got, after)
	}
	if legacyV413Count(after, "echo d76-marked-windsurf") != 0 {
		t.Fatalf("v4.1.3 command-only marker survived the transition:\n%s", after)
	}
	if !strings.Contains(string(after), legacyV413WindsurfManualHandler) {
		t.Fatalf("handwritten handler bytes changed:\n%s", after)
	}

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("deselecting sync: %v", err)
	}
	final, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if legacyV413Count(final, "echo d76-selected") != 0 {
		t.Fatalf("deselection left the managed token:\n%s", final)
	}
	if !strings.Contains(string(final), legacyV413WindsurfManualHandler) {
		t.Fatalf("deselection removed the handwritten handler:\n%s", final)
	}
}
