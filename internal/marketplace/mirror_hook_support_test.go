package marketplace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: marketplace-api/mirrored-hook-support-integrity
func TestMirrorHookSupportIntegrity(t *testing.T) {
	script := hccartifact.SupportFile{Data: []byte("#!/bin/sh\nexit 0\n"), Executable: true}
	for _, test := range []struct {
		name     string
		declared map[string]string
		files    map[string]hccartifact.SupportFile
		valid    bool
	}{
		{"valid", map[string]string{"run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"run.sh": script}, true},
		{"missing", map[string]string{"run.sh": hccartifact.SupportFileDigest(script)}, nil, false},
		{"missing-reference", map[string]string{"helper.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"helper.sh": script}, false},
		{"bytes", map[string]string{"run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"run.sh": {Data: []byte("changed"), Executable: true}}, false},
		{"mode", map[string]string{"run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"run.sh": {Data: script.Data}}, false},
		{"undeclared", map[string]string{"run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"run.sh": script, "extra.sh": script}, false},
		{"traversal", map[string]string{"../outside.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"../outside.sh": script}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := NewCache(t.TempDir())
			digest := "sha256:" + strings.Repeat("a", 64)
			hook := ir.NewHook("guard", "PreToolUse", "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh", "command", nil, ir.SourceRef{})
			hook.SupportFiles = test.declared
			dir, err := stageMirrorResource(cache, digest, hook, hccartifact.CanonicalArtifact{Support: test.files})
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(dir, "run.sh"))
				if err != nil || string(data) != string(script.Data) {
					t.Fatalf("staged script: %q %v", data, err)
				}
				before, err := os.Stat(filepath.Join(dir, "run.sh"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := stageMirrorResource(cache, digest, hook, hccartifact.CanonicalArtifact{Support: test.files}); err != nil {
					t.Fatal(err)
				}
				after, err := os.Stat(filepath.Join(dir, "run.sh"))
				if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
					t.Fatal("intact executable cache was removed or rewritten")
				}
				if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("corrupt"), 0o755); err != nil {
					t.Fatal(err)
				}
				if _, err := stageMirrorResource(cache, digest, hook, hccartifact.CanonicalArtifact{Support: test.files}); err != nil {
					t.Fatal(err)
				}
				if repaired, err := os.ReadFile(filepath.Join(dir, "run.sh")); err != nil || string(repaired) != string(script.Data) {
					t.Fatalf("corrupt support was not repaired: %q %v", repaired, err)
				}
			} else {
				if !errors.Is(err, ErrHookSupportIntegrity) && !errors.Is(err, ErrHookSupportMissing) {
					t.Fatalf("invalid support error identity: %v", err)
				}
				if _, err := os.Stat(cache.MirrorResourceDir(digest)); !os.IsNotExist(err) {
					t.Fatal("invalid support caused a staging write")
				}
			}
		})
	}
}

// spec: marketplace-api/legacy-mirrored-script-hook-refusal
func TestLegacyMirrorHookSupportMissing(t *testing.T) {
	for _, command := range []string{"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh", "${DROID_PLUGIN_ROOT}/hooks/run.sh", "echo safe"} {
		cache := NewCache(t.TempDir())
		hook := ir.NewHook("guard", "PreToolUse", command, "command", nil, ir.SourceRef{})
		ca, err := hccartifact.FromIR(hook)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(ca.Payload), "SupportFiles") {
			t.Fatal("empty declaration changed historical payload")
		}
		_, err = stageMirrorResource(cache, "sha256:"+strings.Repeat("a", 64), hook, ca)
		if command == "echo safe" {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, ErrHookSupportMissing) {
			t.Fatalf("legacy script hook error = %v", err)
		}
		server, contentDigest := serveCanonicalMirror(t, ca, nil)
		cfg := mirrorConsumerConfig(server.URL, contentDigest)
		cfg.Marketplaces[0].Resources[0].Kind = "hook"
		cfg.Marketplaces[0].Resources[0].Name = "guard"
		resolved, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(t.Context(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
		if command == "echo safe" {
			if len(errs) != 0 || len(resolved) != 1 || resolved[0].Resource == nil {
				t.Fatalf("support-free control: %v %v", resolved, errs)
			}
		} else {
			if len(errs) != 1 || !errors.Is(errs[0], ErrHookSupportMissing) {
				t.Fatalf("legacy consumer error: %v", errs)
			}
			for _, artifact := range resolved {
				if artifact.Resource != nil {
					t.Fatal("legacy hook remained materializable")
				}
			}
		}
	}
}
