package harnesscompile

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// No blank imports of compile/<target> or ingest/<target> appear in this file:
// registration is owned by the root package (see register.go), so merely
// building this package must be enough for every declared target to resolve.

func TestTargetRegistryIsComplete(t *testing.T) {
	if err := ValidateTargetRegistrations(); err != nil {
		t.Fatalf("target registration validation failed: %v", err)
	}
}

// TestEveryDeclaredTargetHasItsRegistrations drives the forward direction from
// the registry itself rather than a hardcoded target list, so a newly declared
// target cannot be added without its compiler/ingester registration.
func TestEveryDeclaredTargetHasItsRegistrations(t *testing.T) {
	definitions := targetmeta.Definitions()
	if len(definitions) == 0 {
		t.Fatal("target registry must not be empty")
	}
	for _, definition := range definitions {
		t.Run(string(definition.Target), func(t *testing.T) {
			_, hasCompiler := compile.Lookup(definition.Target)
			if hasCompiler != definition.Compiler {
				t.Errorf("compiler registered = %t, declared Compiler = %t", hasCompiler, definition.Compiler)
			}
			_, hasIngester := ingest.For(definition.Target)
			if hasIngester != definition.Ingester {
				t.Errorf("ingester registered = %t, declared Ingester = %t", hasIngester, definition.Ingester)
			}
		})
	}
}

// TestEveryRegistrationIsDeclared drives the reverse direction: a compiler or
// ingester registered for a target that the registry does not declare (or does
// not declare that capability for) is a wiring bug.
func TestEveryRegistrationIsDeclared(t *testing.T) {
	registeredCompilers := compile.RegisteredTargets()
	if len(registeredCompilers) != len(targetmeta.CompilerTargets()) {
		t.Errorf("registered compilers = %v, declared compiler targets = %v", registeredCompilers, targetmeta.CompilerTargets())
	}
	for _, target := range registeredCompilers {
		definition, ok := targetmeta.Lookup(string(target))
		if !ok {
			t.Errorf("compiler registered for undeclared target %q", target)
			continue
		}
		if !definition.Compiler {
			t.Errorf("compiler registered for target %q which declares Compiler = false", target)
		}
	}

	registeredIngesters := ingest.RegisteredSources()
	if len(registeredIngesters) != len(targetmeta.IngesterTargets()) {
		t.Errorf("registered ingesters = %v, declared ingester targets = %v", registeredIngesters, targetmeta.IngesterTargets())
	}
	for _, source := range registeredIngesters {
		definition, ok := targetmeta.Lookup(string(source))
		if !ok {
			t.Errorf("ingester registered for undeclared target %q", source)
			continue
		}
		if !definition.Ingester {
			t.Errorf("ingester registered for target %q which declares Ingester = false", source)
		}
	}
}

func TestTargetRegistryReturnsIndependentCopies(t *testing.T) {
	definitions := targetmeta.Definitions()
	definitions[0].Label = "mutated"
	fresh, ok := targetmeta.Lookup(string(definitions[0].Target))
	if !ok {
		t.Fatalf("target %q disappeared after mutating a returned copy", definitions[0].Target)
	}
	if fresh.Label == "mutated" {
		t.Fatal("Definitions returned the registry's backing slice")
	}
}
