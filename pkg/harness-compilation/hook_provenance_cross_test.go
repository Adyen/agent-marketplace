package harnesscompile

// Cross-target hook tests that depend on native provenance (OpenSpec change
// native-hook-integration). Kept separate from hook_cross_target_test.go so
// that file compiles without the ir.WithHookSource provenance option.

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: hook-resources/differently-timed-events-are-never-equated
func TestCatalogCrossDifferentlyTimedEventsAreNeverEquated(t *testing.T) {
	// Devin's PostCompaction is documented to run after compaction; Factory
	// documents only a pre-compaction event, so the hook must be suppressed
	// rather than silently retimed.
	hook := ir.NewHookWithMatcherOptions("guard", "PostCompaction", "", "echo compact-check", "command", nil, ir.SourceRef{}, ir.WithHookSource("devin"))
	refuseHook(t, TargetFactory, hook, "hook.event-unmappable")
}
