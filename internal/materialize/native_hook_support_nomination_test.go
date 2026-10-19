package materialize

import (
	"path"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// spec: hook-resources/portable-admission-retains-native-hook-support-boundaries
func TestNativeHookSupportNominationDescriptorBoundary(t *testing.T) {
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(hcc.TargetWindsurf), false)
	if err != nil {
		t.Fatal(err)
	}
	root := path.Join(declaration.partition.Prefix, declaration.aggregate.SupportDir)
	digest := strings.Repeat("a", 64)
	valid := root + "/alpha-" + digest + "/hook/scripts/check.sh"
	if !nativeHookSupportNomination(hcc.TargetWindsurf, false, valid) {
		t.Fatalf("valid support leaf %q was not admitted by the descriptor", valid)
	}
	if err := admitPortableNomination(hcc.TargetWindsurf, false, valid, valid); err != nil {
		t.Fatalf("valid support leaf refused at admission: %v", err)
	}

	sharedAggregate := path.Join(declaration.partition.Prefix, declaration.file.Path)
	refused := map[string]string{
		"uppercase digest": root + "/alpha-" + strings.Repeat("A", 64) + "/hook/scripts/check.sh",
		"short digest":     root + "/alpha-" + strings.Repeat("a", 63) + "/hook/scripts/check.sh",
		"empty slug":       root + "/-" + digest + "/hook/scripts/check.sh",
		"no file leaf":     root + "/alpha-" + digest + "/hook",
		"dot-dot leaf":     root + "/alpha-" + digest + "/hook/../check.sh",
		"outside root":     "other/alpha-" + digest + "/hook/scripts/check.sh",
		"shared aggregate": sharedAggregate,
	}
	for name, candidate := range refused {
		if nativeHookSupportNomination(hcc.TargetWindsurf, false, candidate) {
			t.Errorf("%s %q was admitted by the descriptor", name, candidate)
		}
		if err := admitPortableNomination(hcc.TargetWindsurf, false, candidate, candidate); err == nil {
			t.Errorf("%s %q was admitted at portable admission", name, candidate)
		}
	}

	userDeclaration, err := nativeHookAggregateForScope(targetmeta.Target(hcc.TargetWindsurf), true)
	if err != nil {
		t.Fatal(err)
	}
	userRoot := path.Join(userDeclaration.partition.Prefix, userDeclaration.aggregate.SupportDir)
	userValid := userRoot + "/alpha-" + digest + "/hook/scripts/check.sh"
	if !nativeHookSupportNomination(hcc.TargetWindsurf, true, userValid) {
		t.Fatalf("user-scope relative support leaf %q was not admitted by its descriptor", userValid)
	}
	for _, candidate := range []string{"/" + userValid, ".devin/" + userValid} {
		if nativeHookSupportNomination(hcc.TargetWindsurf, true, candidate) {
			t.Errorf("absolute or repository-prefixed path %q was admitted in the user scope", candidate)
		}
	}
}

// spec: hook-resources/portable-admission-retains-native-hook-support-boundaries
func TestNativeHookSupportSourceKeyBindsProducer(t *testing.T) {
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(hcc.TargetWindsurf), false)
	if err != nil {
		t.Fatal(err)
	}
	root := path.Join(declaration.partition.Prefix, declaration.aggregate.SupportDir)
	for name, res := range map[string]resolver.ResolvedArtifact{
		"git url with dots": {RegistryURL: "https://github.com/acme/agents.v2.git"},
		"local with dots":   {RegistryURL: "/tmp/my.registry/"},
		"marketplace":       {IsMarketplace: true, MarketplaceRegistryID: "corp.registry", MarketplaceResourceName: "audit"},
	} {
		t.Run(name, func(t *testing.T) {
			key := hookSupportSourceKeyFromIdentity(res, "id")
			if !validNativeSupportSourceKey(key) {
				t.Fatalf("producer key %q rejected by the validator", key)
			}
			p := root + "/" + key + "/hook/scripts/check.sh"
			if !nativeHookSupportNomination(hcc.TargetWindsurf, false, p) {
				t.Fatalf("producer key %q not admitted by the descriptor", key)
			}
			if err := admitPortableNomination(hcc.TargetWindsurf, false, p, p); err != nil {
				t.Fatalf("producer key %q refused at admission: %v", key, err)
			}
		})
	}
}

// spec: hook-resources/portable-admission-retains-native-hook-support-boundaries
func TestNativeHookSupportSourceKeyRefusesMalformedSlugs(t *testing.T) {
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(hcc.TargetWindsurf), false)
	if err != nil {
		t.Fatal(err)
	}
	root := path.Join(declaration.partition.Prefix, declaration.aggregate.SupportDir)
	digest := strings.Repeat("a", 64)
	if !validNativeSupportSourceKey(strings.Repeat("a", 64) + "-" + digest) {
		t.Fatal("64-byte slug boundary was refused")
	}
	for name, slug := range map[string]string{
		"leading dot":  ".hidden",
		"trailing dot": "trailing.",
		"dot-dot":      "..",
		"65 bytes":     strings.Repeat("a", 65),
		"illegal rune": "a+b",
	} {
		t.Run(name, func(t *testing.T) {
			key := slug + "-" + digest
			if validNativeSupportSourceKey(key) {
				t.Fatalf("malformed slug key %q was accepted", key)
			}
			p := root + "/" + key + "/hook/scripts/check.sh"
			if nativeHookSupportNomination(hcc.TargetWindsurf, false, p) {
				t.Fatalf("malformed slug path %q was admitted by the descriptor", p)
			}
			if err := admitPortableNomination(hcc.TargetWindsurf, false, p, p); err == nil {
				t.Fatalf("malformed slug path %q was admitted at portable admission", p)
			}
		})
	}
}
