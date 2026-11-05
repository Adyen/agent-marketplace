package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestWindsurfFallbackDuplicateRowsStayRawAndUnowned(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	windsurfFallbackFile(t, repo, `{"hooks":{"pre_run_command":[`+windsurfFallbackHandler+`,`+windsurfFallbackHandler+`]}}`+"\n")
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-selected", "")

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(windsurfFallbackModes())); err != nil {
		t.Fatalf("sync: %v", err)
	}
	data, err := os.ReadFile(windsurfNativeFile(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), windsurfFallbackHandler); got != 2 {
		t.Fatalf("imported manual rows = %d, want both authored rows raw:\n%s", got, data)
	}
	if got := strings.Count(string(data), "echo d77-selected"); got != 1 {
		t.Fatalf("generated rows = %d, want one:\n%s", got, data)
	}
	proof := windsurfNativeProof(t, repo)
	if proof.OwnedCount() != 1 || windsurfProofOwns(proof, windsurfHookIdentity(t, "pre_run_command", windsurfFallbackHandler)) {
		t.Fatalf("owned entries = %d; want only the generated hook", proof.OwnedCount())
	}
}

func TestWindsurfExistingUnownedNativeSatisfiedWithoutImport(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	native := windsurfNativeFile(t, repo)
	seed := `{"hooks":{"pre_run_command":[{"command":"echo d77-same"}]}}` + "\n"
	windsurfFallbackFile(t, repo, seed)
	if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte(seed), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(native, 0o640); err != nil {
		t.Fatal(err)
	}
	before, mode, mtime := fileSnapshot(t, native)
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-same", "")

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(windsurfFallbackModes())); err != nil {
		t.Fatalf("sync: %v", err)
	}
	assertFileUnchanged(t, native, before, mode, mtime)
	if n := windsurfNativeProof(t, repo).OwnedCount(); n != 0 {
		t.Fatalf("existing unowned native entry acquired %d owned entries", n)
	}
}
