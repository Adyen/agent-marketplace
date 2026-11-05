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

func sharedRootHook(t *testing.T, source config.Source, name, definition string) resolver.ResolvedArtifact {
	t.Helper()
	hooks, _ := namedHookFixtures(t, source, hcc.TargetGemini, name, definition)
	if len(hooks) != 1 {
		t.Fatalf("hook fixture %q produced %d artifacts, want 1", name, len(hooks))
	}
	return hooks[0]
}

func TestGeminiSharedRootSelectionRetainsRepoAndPrunesDeselected(t *testing.T) {
	home := configureSharedGeminiHome(t)
	seedNativeHookJSONLock(t, filepath.Join(home, ".gemini"))
	settings := filepath.Join(home, ".gemini", "settings.json")
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	group := func(command string) string {
		return `{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + command + `"}]}]}`
	}
	repoHook := sharedRootHook(t, config.RepoCommitted, "repo-hook", group("echo d78-repo"))
	machineHook := sharedRootHook(t, config.UserPersonal, "machine-hook", group("echo d78-machine"))
	thirdHook := sharedRootHook(t, config.RepoCommitted, "third-hook", group("echo d78-third"))

	if _, err := Run(context.Background(), home, []resolver.ResolvedArtifact{repoHook, machineHook, thirdHook}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	initial, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"echo d78-repo", "echo d78-machine", "echo d78-third"} {
		if got := strings.Count(string(initial), command); got != 1 {
			t.Fatalf("initial %q copies = %d, want 1:\n%s", command, got, initial)
		}
	}
	store := nativeHookMetadataStore(t, filepath.Join(home, ".gemini"))
	if proof := nativeHookMetadataProof(t, store, "settings.json"); proof.OwnedCount() != 3 {
		t.Fatalf("initial owned entries = %d, want 3", proof.OwnedCount())
	}

	unmappableRepo := sharedRootHook(t, config.RepoCommitted, "repo-hook",
		`{"SubagentStop":[{"hooks":[{"type":"command","command":"echo d78-repo"}]}]}`)
	report, err := Run(context.Background(), home, []resolver.ResolvedArtifact{unmappableRepo, machineHook}, WithMaterializationModes(modes))
	if err != nil {
		t.Fatalf("deselecting sync: %v", err)
	}
	refused := false
	for _, d := range report.Diags {
		refused = refused || d.Code == "hook.gemini.event-unmappable"
	}
	if !refused {
		t.Fatalf("unmappable repo event was not refused: %v", report.Diags)
	}
	final, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(final), "echo d78-repo"); got != 1 {
		t.Fatalf("retained repo copies = %d, want 1:\n%s", got, final)
	}
	if got := strings.Count(string(final), "echo d78-machine"); got != 1 {
		t.Fatalf("machine copies = %d, want 1:\n%s", got, final)
	}
	if strings.Contains(string(final), "echo d78-third") {
		t.Fatalf("independently deselected hook survived:\n%s", final)
	}
}
