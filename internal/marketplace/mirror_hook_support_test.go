package marketplace

import (
	"errors"
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
		{"valid", map[string]string{"hooks/run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"hooks/run.sh": script}, true},
		{"missing", map[string]string{"hooks/run.sh": hccartifact.SupportFileDigest(script)}, nil, false},
		{"missing-reference", map[string]string{"helper.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"helper.sh": script}, false},
		{"bytes", map[string]string{"hooks/run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"hooks/run.sh": {Data: []byte("changed"), Executable: true}}, false},
		{"mode", map[string]string{"hooks/run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"hooks/run.sh": {Data: script.Data}}, false},
		{"undeclared", map[string]string{"hooks/run.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"hooks/run.sh": script, "extra.sh": script}, false},
		{"traversal", map[string]string{"../outside.sh": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"../outside.sh": script}, false},
		{"native-definition", map[string]string{"hooks.json": hccartifact.SupportFileDigest(script)}, map[string]hccartifact.SupportFile{"hooks.json": script}, false},
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
				if dir != "" {
					t.Fatalf("mirror hooks must use captured bytes, not a mutable staging tree: %q", dir)
				}
			} else {
				if !errors.Is(err, ErrHookSupportIntegrity) && !errors.Is(err, ErrHookSupportMissing) {
					t.Fatalf("invalid support error identity: %v", err)
				}
			}
		})
	}
}

// spec: marketplace-api/legacy-mirrored-script-hook-refusal
// spec: marketplace-api/mirrored-hook-support-stable-offline-refusal
// spec: marketplace-api/mirrored-hook-support-unsupported-reference-refusal
func TestLegacyMirrorHookSupportMissing(t *testing.T) {
	for _, command := range []string{"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh", "$CLAUDE_PLUGIN_ROOT/hooks/run.sh", "${DROID_PLUGIN_ROOT}/hooks/run.sh", "${CLAUDE_PLUGIN_ROOT}/hooks/lib/run.sh", "${CLAUDE_PLUGIN_ROOT}/scripts/run.sh", "cd ${CLAUDE_PLUGIN_ROOT} && ./x.sh", "echo safe"} {
		wantErr := ErrHookSupportMissing
		if strings.Contains(command, "/hooks/lib/") || strings.Contains(command, "/scripts/") || strings.HasPrefix(command, "cd ") {
			wantErr = ErrHookSupportUnsupported
		}
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
		} else if !errors.Is(err, wantErr) {
			t.Fatalf("legacy script hook error = %v", err)
		}
		server, contentDigest := serveCanonicalMirror(t, ca, nil)
		cfg := mirrorConsumerConfig(server.URL, contentDigest)
		cfg.Marketplaces[0].Resources[0].Kind = "hook"
		cfg.Marketplaces[0].Resources[0].Name = "guard"
		consumer := &Resolver{Cache: NewCache(t.TempDir())}
		resolved, errs := consumer.Resolve(t.Context(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
		if command == "echo safe" {
			if len(errs) != 0 || len(resolved) != 1 || resolved[0].Resource == nil {
				t.Fatalf("support-free control: %v %v", resolved, errs)
			}
		} else {
			if len(errs) != 1 || !errors.Is(errs[0], wantErr) {
				t.Fatalf("legacy consumer error: %v", errs)
			}
			for _, artifact := range resolved {
				if artifact.Resource != nil {
					t.Fatal("legacy hook remained materializable")
				}
			}
			_, errs = consumer.Resolve(t.Context(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeCachedNoFetch)
			if len(errs) != 1 || !errors.Is(errs[0], wantErr) {
				t.Fatalf("offline refusal lost its stable code: %v", errs)
			}
		}
	}
}
