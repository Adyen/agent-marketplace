package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// spec: application-target-model/current-harness-targets-accept-every-selectable-kind
// spec: application-target-model/unaccepted-kind-is-refused-before-output
func TestAcceptedKindsCompletenessAndLookup(t *testing.T) {
	registry, err := NewInstallationAdapterRegistry()
	if err != nil {
		t.Fatal(err)
	}

	definitions := targetmeta.Definitions()
	targets := registry.Targets()
	if len(targets) != len(definitions) {
		t.Fatalf("application targets = %d, compiled definitions = %d", len(targets), len(definitions))
	}
	for i, definition := range definitions {
		expectedID := string(definition.Target)
		if targets[i].ID != expectedID {
			t.Fatalf("application target %d = %q, want %q", i, targets[i].ID, expectedID)
		}
		appTarget, ok := registry.Lookup(expectedID)
		if !ok {
			t.Fatalf("application target %q is missing from lookup", expectedID)
		}
		for _, kind := range config.AllKinds {
			want := kind != ir.KindCollection
			if got := appTarget.Accepts(kind); got != want {
				t.Errorf("%s accepts %s = %t, want %t", expectedID, kind, got, want)
			}
		}
		if !appTarget.Accepts(ir.KindPlugin) {
			t.Errorf("%s does not accept plugin packages", expectedID)
		}
		wantRule := definition.Target == targetmeta.TargetFactory ||
			definition.Target == targetmeta.TargetCursor ||
			definition.Target == targetmeta.TargetWindsurf
		if got := appTarget.Accepts(ir.KindRule); got != wantRule {
			t.Errorf("%s accepts rule = %t, want %t", expectedID, got, wantRule)
		}
	}
	if _, ok := registry.Lookup("not-a-target"); ok {
		t.Fatal("lookup returned an undeclared application target")
	}

	partial := NewApplicationTarget("partial", []ir.ResourceKind{ir.KindSkill})
	if !partial.Accepts(ir.KindSkill) {
		t.Fatal("partial target rejected its declared skill kind")
	}
	if partial.Accepts(ir.KindHook) {
		t.Fatal("partial target accepted an undeclared hook kind")
	}
	err = partial.RequireAccepted(ir.KindHook)
	if err == nil {
		t.Fatal("partial target did not refuse its undeclared hook kind")
	}
	for _, name := range []string{"hook", "partial"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("refusal %q does not name %q", err, name)
		}
	}
}

// spec: application-target-model/existing-harness-output-is-unchanged
func TestCurrentHarnessAcceptedKindOutputUnchanged(t *testing.T) {
	want := map[string]string{
		"claude":   "665ac9ecf4f0993c24c7ed5620b36f97079767912b961d4bf1554fe1b89c5a09",
		"factory":  "665ac9ecf4f0993c24c7ed5620b36f97079767912b961d4bf1554fe1b89c5a09",
		"cursor":   "665ac9ecf4f0993c24c7ed5620b36f97079767912b961d4bf1554fe1b89c5a09",
		"windsurf": "0997dc23961954afab53af43e8ac88acba7b431bfe69b0a0260d740cf7cd9fda",
		"gemini":   "64ffee8111f5f246c4f9bf131b8a1f57ae709715833c7a9ab540a91ecd93377d",
		"devin":    "665ac9ecf4f0993c24c7ed5620b36f97079767912b961d4bf1554fe1b89c5a09",
		"codex":    "7ca5983f7c74752e2a5ad2bba4ddabb5a674a473eabc5c83404a7bb0eb7cbdac",
		"opencode": "cc0250c51ddd7bedb046ff125fa891c74d207bbfb825148c3dd79ef804e3bbbf",
	}
	registry, err := NewInstallationAdapterRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resource := ir.NewSkill("accepted-kind-fixture", "A fixed fixture.", "Keep output stable.", nil, nil, ir.SourceRef{})
	for _, appTarget := range registry.Targets() {
		result, err := hcc.Compile(resource, hcc.Target(appTarget.ID))
		if err != nil {
			t.Fatalf("compile fixture for %s: %v", appTarget.ID, err)
		}
		if got := artifactDigest(result.Artifacts); got != want[appTarget.ID] {
			t.Errorf("%s output digest = %s, want %s", appTarget.ID, got, want[appTarget.ID])
		}
		delete(want, appTarget.ID)
	}
	if len(want) != 0 {
		t.Errorf("output checks missing targets: %v", want)
	}
}

func artifactDigest(artifacts []hcc.Artifact) string {
	hash := sha256.New()
	for _, artifact := range artifacts {
		_, _ = hash.Write(fmt.Appendf(nil, "%s\x00%d\x00", artifact.Path, artifact.Mode))
		_, _ = hash.Write(artifact.Bytes)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
