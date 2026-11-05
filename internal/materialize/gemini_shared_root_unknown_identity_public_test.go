package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestGeminiSharedRootUnknownIdentityWithholdsPruneAcrossScopes(t *testing.T) {
	home := configureSharedGeminiHome(t)
	seedNativeHookJSONLock(t, filepath.Join(home, ".gemini"))
	settings := filepath.Join(home, ".gemini", "settings.json")
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	group := func(command string) string {
		return `{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + command + `"}]}]}`
	}
	repoHook := sharedRootHook(t, config.RepoCommitted, "repo-hook", group("echo d79-repo"))
	machineHook := sharedRootHook(t, config.UserPersonal, "machine-hook", group("echo d79-machine"))
	thirdHook := sharedRootHook(t, config.RepoCommitted, "third-hook", group("echo d79-third"))

	if _, err := Run(context.Background(), home, []resolver.ResolvedArtifact{repoHook, machineHook, thirdHook}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	store := nativeHookMetadataStore(t, filepath.Join(home, ".gemini"))
	if proof := nativeHookMetadataProof(t, store, "settings.json"); proof.OwnedCount() != 3 {
		t.Fatalf("initial owned entries = %d, want 3", proof.OwnedCount())
	}

	identityless := repoHook
	identityless.RegistryURL = ""
	report, err := Run(context.Background(), home, []resolver.ResolvedArtifact{identityless, machineHook}, WithMaterializationModes(modes))
	if err != nil {
		t.Fatalf("identityless sync: %v", err)
	}
	if !diag.HasCode(report.Diags, "materialize.compile.error") {
		t.Fatalf("identityless repo hook was not refused: %v", report.Diags)
	}
	final, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"echo d79-repo", "echo d79-machine", "echo d79-third"} {
		if got := strings.Count(string(final), command); got != 1 {
			t.Fatalf("%q copies after identityless sync = %d, want 1:\n%s", command, got, final)
		}
	}
}
