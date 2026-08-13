package ingest_test

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
)

// The Devin autodetection precedence rule, stated once:
//
//	A canonical `.devin` workspace marker beats EVERY other layout marker,
//	including the explicit Claude/Factory/Cursor plugin manifests and the Gemini
//	extension manifest. A direct `~/.config/devin`-shaped root is never
//	autodetected at all.
//
// Plan, "Bidirectional conversion": "When `.devin` and compatibility layouts
// coexist at the explicit root, autodetection selects Devin; callers may
// explicitly choose another source harness." Evidence baseline, "Compatibility
// imports": "Autodetection prefers `.devin` when it coexists with compatibility
// markers."

const devinSkill = "---\nname: demo-skill\ndescription: Demo skill\n---\nDo the thing\n"

// devinWorkspace returns the canonical `.devin` markers, one per call site, so a
// test can add competing layouts around them.
func devinWorkspace() fstest.MapFS {
	return fstest.MapFS{
		".devin/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
	}
}

// competingLayouts is every marker autodetection would otherwise honor. Each one
// is a compatibility layout Devin itself can read, or a sibling harness's own
// layout, so none of them may beat a canonical `.devin` marker.
var competingLayouts = map[string]fstest.MapFS{
	"claude plugin manifest": {
		".claude-plugin/plugin.json": &fstest.MapFile{Data: []byte(`{"name":"demo"}`)},
		"skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
	},
	"factory plugin manifest": {
		".factory-plugin/plugin.json": &fstest.MapFile{Data: []byte(`{"skills":[{"name":"demo-skill","description":""}]}`)},
	},
	"cursor plugin manifest": {
		".cursor-plugin/plugin.json": &fstest.MapFile{Data: []byte(`{"name":"demo"}`)},
	},
	"gemini extension manifest": {
		"gemini-extension.json": &fstest.MapFile{Data: []byte(`{"name":"demo","version":"1.0.0"}`)},
	},
	"claude workspace": {
		".claude/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
	},
	"factory workspace": {
		".factory/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
	},
	"cursor workspace": {
		".cursor/rules/demo.mdc": &fstest.MapFile{Data: []byte("---\ndescription: d\nalwaysApply: true\n---\nBody\n")},
	},
	"gemini workspace": {
		".gemini/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
	},
	"windsurf workspace": {
		".windsurf/rules/demo.md": &fstest.MapFile{Data: []byte("---\ntrigger: always_on\n---\nBody\n")},
	},
	"agents compatibility skills": {
		".agents/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
	},
	"bare skills directory": {
		"demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
	},
}

// TestAutodetectDevinWorkspaceBeatsEveryOtherMarker walks each competing layout
// individually so a failure names exactly which marker won.
func TestAutodetectDevinWorkspaceBeatsEveryOtherMarker(t *testing.T) {
	for name, competitor := range competingLayouts {
		t.Run(name, func(t *testing.T) {
			// Control: without the Devin markers this layout must NOT be
			// detected as Devin. Without this half the test would pass on an
			// autodetector that always answered Devin.
			if detected, ok := ingest.Autodetect(competitor); ok && detected == ingest.HarnessDevin {
				t.Fatalf("control: %s alone was detected as devin", name)
			}

			src := devinWorkspace()
			for path, entry := range competitor {
				src[path] = entry
			}
			detected, ok := ingest.Autodetect(src)
			if !ok {
				t.Fatal("Autodetect detected nothing")
			}
			if detected != ingest.HarnessDevin {
				t.Fatalf("Autodetect = %q, want %q: a canonical .devin marker must beat the %s marker", detected, ingest.HarnessDevin, name)
			}
			// Strict detection must agree: sub-bundle scanning and top-level
			// autodetection may not disagree about what a Devin root is.
			if detected, ok := ingest.AutodetectStrict(src); !ok || detected != ingest.HarnessDevin {
				t.Fatalf("AutodetectStrict = (%q, %v), want (%q, true)", detected, ok, ingest.HarnessDevin)
			}
		})
	}
}

// TestAutodetectDevinPrecedenceIsOrderIndependent stacks EVERY competing layout
// into one workspace at once and repeats the probe.
//
// The repetition is the point: Go randomizes map iteration, so a detector that
// consulted a directory listing or a map would produce a different winner on
// different runs. A fixed, ordered list of literal marker paths cannot.
func TestAutodetectDevinPrecedenceIsOrderIndependent(t *testing.T) {
	src := devinWorkspace()
	for _, competitor := range competingLayouts {
		for path, entry := range competitor {
			src[path] = entry
		}
	}
	// Every canonical Devin marker at once, so no single marker is load-bearing.
	src[".devin/agents/demo-agent.md"] = &fstest.MapFile{Data: []byte("---\nname: demo-agent\ndescription: d\n---\nBody\n")}
	src[".devin/hooks.v1.json"] = &fstest.MapFile{Data: []byte(`{}`)}
	src[".devin/mcp_config.json"] = &fstest.MapFile{Data: []byte(`{}`)}
	src[".devin/config.json"] = &fstest.MapFile{Data: []byte(`{}`)}

	for run := 0; run < 100; run++ {
		detected, ok := ingest.Autodetect(src)
		if !ok || detected != ingest.HarnessDevin {
			t.Fatalf("run %d: Autodetect = (%q, %v), want (%q, true)", run, detected, ok, ingest.HarnessDevin)
		}
	}
}

// TestAutodetectRefusesTheDirectDevinRoot is the user-root safety invariant at
// the autodetection boundary: the `~/.config/devin` shape — which is also the
// shape the Devin compiler materializes into a target root — must never be
// picked up without an explicit source harness.
func TestAutodetectRefusesTheDirectDevinRoot(t *testing.T) {
	tests := map[string]fstest.MapFS{
		"skills only": {"skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)}},
		"agents only": {"agents/demo-agent.md": &fstest.MapFile{Data: []byte("---\nname: demo-agent\ndescription: d\n---\nBody\n")}},
		"config only": {"config.json": &fstest.MapFile{Data: []byte(`{"hooks":{}}`)}},
		"mcp only":    {"mcp_config.json": &fstest.MapFile{Data: []byte(`{"mcpServers":{}}`)}},
		"hooks only":  {"hooks.v1.json": &fstest.MapFile{Data: []byte(`{}`)}},
		"complete user root": {
			"skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
			"agents/demo-agent.md":       &fstest.MapFile{Data: []byte("---\nname: demo-agent\ndescription: d\n---\nBody\n")},
			"config.json":                &fstest.MapFile{Data: []byte(`{}`)},
			"mcp_config.json":            &fstest.MapFile{Data: []byte(`{}`)},
		},
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			if detected, ok := ingest.Autodetect(src); ok && detected == ingest.HarnessDevin {
				t.Fatal("Autodetect selected devin for a direct root; the plan requires an explicit --from devin or source_harness: devin")
			}
			if detected, ok := ingest.AutodetectStrict(src); ok && detected == ingest.HarnessDevin {
				t.Fatal("AutodetectStrict selected devin for a direct root")
			}
			// The explicit path still works: this is a selection rule, not a
			// refusal to read the layout.
			ing, ok := ingest.For(ingest.HarnessDevin)
			if !ok {
				t.Fatal("no devin ingester registered")
			}
			if _, err := ing.LoadBundle(src); err != nil {
				t.Fatalf("LoadBundle refused an explicitly selected direct root: %v", err)
			}
		})
	}
}

// TestAutodetectDoesNotDiscoverNestedDevinRoots pins "no runtime ancestor/nested
// merge occurs" at the autodetection boundary. A `.devin` one level down is not
// a marker for THIS root; if it were, converting a monorepo root would silently
// pull in one package's Devin configuration.
func TestAutodetectDoesNotDiscoverNestedDevinRoots(t *testing.T) {
	tests := map[string]fstest.MapFS{
		"one level down":      {"sub/.devin/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)}},
		"two levels down":     {"a/b/.devin/mcp_config.json": &fstest.MapFile{Data: []byte(`{}`)}},
		"inside .devin":       {".devin/.devin/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)}},
		"inside another root": {".claude/.devin/mcp_config.json": &fstest.MapFile{Data: []byte(`{}`)}},
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			if detected, ok := ingest.Autodetect(src); ok && detected == ingest.HarnessDevin {
				t.Fatal("Autodetect walked into a nested .devin directory")
			}
			if detected, ok := ingest.AutodetectStrict(src); ok && detected == ingest.HarnessDevin {
				t.Fatal("AutodetectStrict walked into a nested .devin directory")
			}
		})
	}
}
