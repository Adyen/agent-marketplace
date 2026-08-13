// agent-marketplace-integration demonstrates the recommended per-skill
// invocation pattern for consuming the harness-compilation library,
// matching the design doc's §Integration with agent-marketplace section.
//
// agent-marketplace discovers skills in its registries (each typically a
// loose SKILL.md). This example shows how to compile each discovered skill
// into a target harness format and materialize it to the appropriate
// install root.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	// The root package registers every built-in compiler and ingester, so this
	// single import is all a consumer needs.
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func main() {
	// Simulate agent-marketplace's per-skill, per-target compilation loop.
	// In production, the FS and paths come from the registry.
	pluginDir := "testdata/resources"
	if len(os.Args) > 1 {
		pluginDir = os.Args[1]
	}

	targets := []hcc.Target{
		hcc.TargetClaude,
		hcc.TargetFactory,
		hcc.TargetCursor,
		hcc.TargetWindsurf,
		hcc.TargetGemini,
	}

	for _, target := range targets {
		fmt.Printf("=== Compiling for %s ===\n", target)

		// Option 1: Compile the entire bundle at once.
		src := os.DirFS(pluginDir)
		result, err := hcc.CompileBundle(src, target, hcc.WithPluginRoot(pluginDir))
		if err != nil {
			fmt.Fprintf(os.Stderr, "CompileBundle(%s): %v\n", target, err)
			continue
		}

		// Print diagnostics.
		for _, d := range result.Diagnostics {
			fmt.Printf("  [%s] %s: %s\n", d.Severity, d.Code, d.Message)
		}

		// Materialize to a per-target output directory.
		outDir := fmt.Sprintf("/tmp/hcc-example/%s", target)
		report, err := hcc.Materialize(result, outDir,
			hcc.WithWriteMode(hcc.WriteModeSync),
			hcc.WithPruneStale(true),
		)
		// A partial result is not a failure to materialize: everything that
		// compiled cleanly is on disk. It is still not a clean run, so it must
		// be reported as such rather than folded into the success line.
		if err != nil && !errors.Is(err, hcc.ErrPartialResult) {
			fmt.Fprintf(os.Stderr, "Materialize(%s): %v\n", target, err)
			continue
		}

		fmt.Printf("  Written: %d  Skipped: %d  Removed: %d\n",
			len(report.Written), len(report.Skipped), len(report.Removed))
		if report.Partial {
			for _, s := range report.SkippedResources {
				fmt.Printf("  PARTIAL: skipped %s %q (%s)\n", s.Kind, s.Name, strings.Join(s.Codes, ", "))
			}
		}
	}

	fmt.Println("\n--- Per-skill pattern (agent-marketplace integration) ---")

	// Option 2: Per-skill invocation pattern from the design doc. This is what
	// agent-marketplace does for each registry skill, using the documented
	// root-level loaders.
	registryFS := os.DirFS(pluginDir)
	skill, err := hcc.LoadSkill(registryFS, "skills/demo-skill")
	if err != nil {
		fmt.Fprintf(os.Stderr, "LoadSkill: %v\n", err)
		return
	}
	result, err := hcc.Compile(skill, hcc.TargetFactory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Compile(%s, factory): %v\n", skill.Name(), err)
		return
	}
	report, err := hcc.Materialize(result, "/tmp/hcc-example/per-skill/.factory/skills",
		hcc.WithWriteMode(hcc.WriteModeSync))
	partial := errors.Is(err, hcc.ErrPartialResult)
	if err != nil && !partial {
		fmt.Fprintf(os.Stderr, "Materialize(%s): %v\n", skill.Name(), err)
		return
	}
	fmt.Printf("  %s → factory: written %d, skipped %d\n", skill.Name(), len(report.Written), len(report.Skipped))
	if partial {
		fmt.Fprintf(os.Stderr, "Materialize(%s): %v\n", skill.Name(), err)
		os.Exit(1)
	}
}
