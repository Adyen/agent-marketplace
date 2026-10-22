package materialize

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: hook-resources/handwritten-matches-and-unavailable-proof-cannot-authorize-removal
// spec: hook-resources/remaining-contributors-and-owned-removal-stay-supported
func TestCheckHookRemoval(t *testing.T) {
	for _, layout := range nativeHookJSONLayoutCases() {
		if layout.machineScope {
			continue
		}
		target := layout.target
		for _, condition := range []string{"owned", "missing-state", "corrupt-state", "handwritten", "retained", "absent", "other-scope", "different-matcher"} {
			if target == hcc.TargetWindsurf && condition == "different-matcher" {
				continue // Windsurf does not have a native matcher field.
			}
			t.Run(string(target)+"/"+condition, func(t *testing.T) {
				stateRoot := isolateIndependentHookRun(t)
				repo := t.TempDir()
				if HasActiveNativeHooks(repo) {
					t.Fatal("empty native roots require no hook preflight")
				}
				const command = nativeHookJSONSelectedCommand
				fixture := writeProjectionSource(t, command)
				fixture.resolved.Harnesses = []hcc.Target{target}
				before := []resolver.ResolvedArtifact{fixture.resolved}
				key := outputKey{target: target}
				destination, file, err := nativeHookJSONLayout(key, repo)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(destination.Root, file.rel)
				var after []resolver.ResolvedArtifact
				refused := condition == "missing-state" || condition == "corrupt-state" || condition == "handwritten"
				switch condition {
				case "absent":
				case "handwritten", "different-matcher":
					layout := nativeHookJSONCaseFor(t, target, false)
					if condition == "different-matcher" {
						// Same command, but a different matcher is not the removed entry.
						layout.matcher = "different-native-tool"
					}
					body, _, _, _ := nativeHookJSONIdenticalSeed(layout)
					if target == hcc.TargetCursor {
						handwritten := fmt.Sprintf(`{"type":"command","matcher":%q,"command":%q}`, layout.matcher, command)
						body, _ = nativeHookJSONDocument(layout, handwritten, nativeHookJSONFlatHandler(nativeHookJSONOtherCommand, layout.matcher))
					}
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, body, 0o640); err != nil {
						t.Fatal(err)
					}
				default:
					report, err := Run(context.Background(), repo, before)
					if err != nil || diag.HasErrors(report.Diags) {
						t.Fatalf("install: %v / %+v", err, report)
					}
					store, err := ownership.Open(destination.Root)
					if err != nil {
						t.Fatal(err)
					}
					switch condition {
					case "missing-state":
						if err := os.Remove(store.StateFile()); err != nil {
							t.Fatal(err)
						}
					case "corrupt-state":
						if err := os.WriteFile(store.StateFile(), []byte("{corrupt"), 0o600); err != nil {
							t.Fatal(err)
						}
					case "retained":
						other := writeProjectionSource(t, command).resolved
						other.Harnesses = []hcc.Target{target}
						after = []resolver.ResolvedArtifact{other}
						t.Setenv("XDG_STATE_HOME", t.TempDir())
					case "other-scope":
						before[0].Source = config.UserPersonal
					}
				}
				var nativeBefore []byte
				var nativeMode os.FileMode
				var nativeMtime time.Time
				if condition != "absent" {
					nativeBefore, nativeMode, nativeMtime = fileSnapshot(t, path)
				}
				if active := HasActiveNativeHooks(repo); active != (condition != "absent") {
					t.Fatalf("active native hooks = %v for %s", active, condition)
				}
				store, err := ownership.Open(destination.Root)
				if err != nil {
					t.Fatal(err)
				}
				stateBefore, stateErr := os.ReadFile(store.StateFile())
				stateInfo, _ := os.Stat(store.StateFile())
				diagnostics := CheckHookRemoval(repo, before, after)
				stateAfter, afterErr := os.ReadFile(store.StateFile())
				if errors.Is(stateErr, os.ErrNotExist) {
					if !errors.Is(afterErr, os.ErrNotExist) {
						t.Fatalf("preflight created ownership state: %v", afterErr)
					}
				} else {
					info, err := os.Stat(store.StateFile())
					if stateErr != nil || afterErr != nil || err != nil || string(stateAfter) != string(stateBefore) ||
						!os.SameFile(stateInfo, info) || stateInfo.Mode() != info.Mode() || stateInfo.ModTime() != info.ModTime() {
						t.Fatalf("preflight changed ownership state: %v / %v / %v", stateErr, afterErr, err)
					}
				}
				if got := diag.HasCode(diagnostics, "hook.remove-unowned"); got != refused {
					t.Fatalf("refused = %v, want %v: %+v", got, refused, diagnostics)
				}
				for _, diagnostic := range diagnostics {
					if diagnostic.Severity != diag.SeverityError || diagnostic.Target != string(target) || diagnostic.Source.File != path ||
						strings.Contains(diagnostic.Message+diagnostic.Hint, command) {
						t.Fatalf("unsafe or unattributed diagnostic: %+v", diagnostic)
					}
				}
				if condition != "absent" {
					assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
				}
				if condition == "absent" || condition == "handwritten" || condition == "different-matcher" {
					if _, err := os.Stat(stateRoot); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("read-only preflight created state: %v", err)
					}
				}
			})
		}
	}
}

// spec: hook-resources/remaining-contributors-and-owned-removal-stay-supported
func TestCheckHookRemovalMissingSupportReferenceDoesNotBlock(t *testing.T) {
	isolateIndependentHookRun(t)
	repo := t.TempDir()
	fixture := writeProjectionSource(t, `${CLAUDE_PLUGIN_ROOT}/hooks/guard.sh`)
	if err := os.WriteFile(filepath.Join(fixture.root, "hooks", "keep.sh"), []byte("echo keep\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.resolved.Harnesses = []hcc.Target{hcc.TargetFactory}
	before := []resolver.ResolvedArtifact{fixture.resolved}
	if report, err := Run(context.Background(), repo, before); err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("install: %v / %+v", err, report)
	}
	if err := os.Remove(fixture.support); err != nil {
		t.Fatal(err)
	}
	if diagnostics := CheckHookRemoval(repo, before, nil); len(diagnostics) != 0 {
		t.Fatalf("removal of a hook whose support reference went missing was blocked: %+v", diagnostics)
	}
}

// spec: hook-resources/file-based-hook-targets-keep-ordinary-removal-behavior
func TestCheckHookRemovalFileBasedTarget(t *testing.T) {
	repo := openCodeSandbox(t)
	t.Setenv("CODEX_HOME", filepath.Join(os.Getenv("HOME"), ".codex"))
	fixture := writeProjectionSource(t, "echo file-based-hook")
	fixture.resolved.Harnesses = []hcc.Target{hcc.TargetOpenCode}
	before := []resolver.ResolvedArtifact{fixture.resolved}
	report, err := Run(context.Background(), repo, before)
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("install: %v / %+v", err, report)
	}
	declaration := filepath.Join(repo, ".opencode", "hooks", "agent-marketplace", "PreToolUse-command.json")
	adapter := filepath.Join(repo, ".opencode", "plugins", "agent-marketplace.js")
	declarationBytes, declarationMode, declarationMtime := fileSnapshot(t, declaration)
	adapterBytes, adapterMode, adapterMtime := fileSnapshot(t, adapter)

	if HasActiveNativeHooks(repo) {
		t.Error("file-based hooks must not activate the native aggregate preflight")
	}
	if diagnostics := CheckHookRemoval(repo, before, nil); len(diagnostics) != 0 {
		t.Errorf("aggregate preflight refused file-based hook removal: %+v", diagnostics)
	}
	assertFileUnchanged(t, declaration, declarationBytes, declarationMode, declarationMtime)
	assertFileUnchanged(t, adapter, adapterBytes, adapterMode, adapterMtime)
}
