package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: marketplace-api/mirrored-hook-support-materialization-refusal
func TestMirrorHookSupportRefusesUnavailableOrReservedFiles(t *testing.T) {
	for _, name := range []string{"missing", "missing-inventory", "reserved", "index-only", "tampered", "duplicate", "valid"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			root := t.TempDir()
			source := filepath.Join(root, "hooks.json")
			if err := os.WriteFile(source, []byte(`{"hooks":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			skill := mirrorHookSafetyFixture(source)
			hook := skill.Resource.(*ir.Hook)
			file := "hooks/run.sh"
			if name == "reserved" {
				file = "hooks.json"
			}
			data := []byte("echo safe\n")
			if name == "reserved" {
				data = []byte(`{"hooks":{}}`)
			}
			hook.SupportFiles = map[string]string{file: artifact.SupportFileDigest(artifact.SupportFile{Data: data})}
			if name != "missing" && name != "missing-inventory" {
				skill.MarketplaceHookSupport = []artifact.Artifact{{Path: file, Bytes: data, Mode: 0o644}}
			}
			if name != "missing" && name != "reserved" {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, file)), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, file), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "index-only" {
				skill.MarketplaceAcquisition = config.MarketplaceAcquisitionIndexOnly
			}
			if name == "tampered" {
				skill.MarketplaceHookSupport[0].Bytes = []byte("tampered")
			}
			if name == "duplicate" {
				skill.MarketplaceHookSupport = append(skill.MarketplaceHookSupport, skill.MarketplaceHookSupport[0])
			}
			repo := t.TempDir()
			native := filepath.Join(repo, ".factory", "hooks.json")
			previous := []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo previous"}]}]}}`)
			if name != "valid" {
				if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(native, previous, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{skill}, WithRepoScopeOnly())
			if err != nil {
				t.Fatal(err)
			}
			if name == "valid" {
				if diag.HasErrors(report.Diags) {
					t.Fatalf("valid hook refused: %v", report.Diags)
				}
			} else if name == "duplicate" && !diag.HasCode(report.Diags, "materialize.compile.error") ||
				name != "duplicate" && !diag.HasCode(report.Diags, diag.CodeMaterializeHookSupportRefused) {
				t.Fatalf("unsafe support was not refused: %v", report.Diags)
			} else {
				if data, err := os.ReadFile(native); err != nil || string(data) != string(previous) {
					t.Fatalf("prior hook changed on refusal: %q, %v", data, err)
				}
				if paths, err := filepath.Glob(filepath.Join(repo, ".factory", "hook-support", "*", "*", "*")); err != nil || len(paths) != 0 {
					t.Fatalf("refusal emitted support: %v, %v", paths, err)
				}
			}
		})
	}
}

// spec: marketplace-api/mirrored-hook-support-unowned-destination-refusal
func TestMirrorHookSupportMissingOwnershipRefusesAggregate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	repo, source := t.TempDir(), t.TempDir()
	data := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(filepath.Join(source, "run.sh"), data, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := mirrorHookSafetyFixture(filepath.Join(source, "hooks.json"))
	hook := skill.Resource.(*ir.Hook)
	hook.Command = "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh"
	hook.SupportFiles = map[string]string{"hooks/run.sh": artifact.SupportFileDigest(artifact.SupportFile{Data: data, Executable: true})}
	skill.MarketplaceHookSupport = []artifact.Artifact{{Path: "hooks/run.sh", Bytes: data, Mode: 0o755}}
	selected := []resolver.ResolvedArtifact{skill}
	report, err := Run(t.Context(), repo, selected, WithRepoScopeOnly())
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("valid control: %v, %v", report, err)
	}
	native := filepath.Join(repo, ".factory", "hooks.json")
	before, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, ".agents", "materialization-manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	report, err = Run(t.Context(), repo, selected, WithRepoScopeOnly())
	if err != nil || !diag.HasCode(report.Diags, diag.CodeMaterializeHookSupportConflict) {
		t.Fatalf("unowned support not refused: %v, %v", report, err)
	}
	if after, err := os.ReadFile(native); err != nil || string(after) != string(before) {
		t.Fatalf("aggregate changed on destination refusal: %q, %v", after, err)
	}
}

func mirrorHookSafetyFixture(source string) resolver.ResolvedArtifact {
	hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	return resolver.ResolvedArtifact{
		Entry:         catalog.Entry{Name: "guard", Kind: ir.KindHook, Path: source, Resource: hook, SourceHarness: "claude"},
		IsMarketplace: true, MarketplaceAcquisition: config.MarketplaceAcquisitionMirror,
		MarketplaceInstanceID: "test", MarketplaceRegistryID: "registry",
		MarketplaceResourceName: "guard", MarketplaceDigest: "sha256:" + strings.Repeat("a", 64),
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
	}
}
