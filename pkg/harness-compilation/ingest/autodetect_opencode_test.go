package ingest_test

import (
	"maps"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/opencode"
)

func openCodeMarker() fstest.MapFS {
	return fstest.MapFS{".opencode/commands/check.md": &fstest.MapFile{Data: []byte("Check\n")}}
}

func withLayouts(layouts ...fstest.MapFS) fstest.MapFS {
	merged := fstest.MapFS{}
	for _, layout := range layouts {
		maps.Copy(merged, layout)
	}
	return merged
}

func autodetectAmbiguity(t *testing.T, src fstest.MapFS) (ingest.Harness, bool, bool) {
	t.Helper()
	h, ok, diagnostics := ingest.AutodetectWithDiagnostics(src)
	if plain, plainOK := ingest.Autodetect(src); plain != h || plainOK != ok {
		t.Fatalf("Autodetect = (%q, %v), AutodetectWithDiagnostics = (%q, %v)", plain, plainOK, h, ok)
	}
	ambiguous := diag.HasCode(diagnostics, diag.CodeIngestAutodetectAmbiguous)
	for _, d := range diagnostics {
		if d.Code != diag.CodeIngestAutodetectAmbiguous || d.Severity != diag.SeverityWarning {
			t.Fatalf("unexpected autodetect diagnostic %#v", d)
		}
	}
	return h, ok, ambiguous
}

// spec: harness-opencode/existing-workspace-layouts-keep-precedence-over-an-opencode-marker
// spec: harness-opencode/coexisting-layouts-are-reported
func TestAutodetectExistingWorkspaceLayoutsKeepPrecedenceOverOpenCode(t *testing.T) {
	for name, want := range map[string]ingest.Harness{
		"claude workspace":   ingest.HarnessClaude,
		"factory workspace":  ingest.HarnessFactory,
		"cursor workspace":   ingest.HarnessCursor,
		"gemini workspace":   ingest.HarnessGemini,
		"windsurf workspace": ingest.HarnessWindsurf,
	} {
		t.Run(name, func(t *testing.T) {
			layout := competingLayouts[name]
			if h, ok := ingest.Autodetect(layout); !ok || h != want {
				t.Fatalf("control: %s alone = (%q, %v), want %q", name, h, ok, want)
			}
			h, ok, ambiguous := autodetectAmbiguity(t, withLayouts(layout, openCodeMarker()))
			if !ok || h != want || !ambiguous {
				t.Fatalf("%s + .opencode = (%q, %v, ambiguous=%v), want %q reported as ambiguous", name, h, ok, ambiguous, want)
			}
			if h, ok := ingest.AutodetectStrict(withLayouts(layout, openCodeMarker())); !ok || h != want {
				t.Fatalf("strict %s + .opencode = (%q, %v), want %q", name, h, ok, want)
			}
		})
	}
	codex := fstest.MapFS{
		".codex/config.toml":           &fstest.MapFile{Data: []byte("")},
		".agents/skills/demo/SKILL.md": &fstest.MapFile{Data: []byte(devinSkill)},
		".opencode/agents/reviewer.md": &fstest.MapFile{Data: []byte("---\ndescription: Review\nmode: subagent\n---\nReview\n")},
		".opencode/package.json":       &fstest.MapFile{Data: []byte("{}")},
	}
	if h, ok, ambiguous := autodetectAmbiguity(t, codex); !ok || h != ingest.HarnessCodex || !ambiguous {
		t.Fatalf("codex + .opencode = (%q, %v, ambiguous=%v), want codex reported as ambiguous", h, ok, ambiguous)
	}
}

// spec: harness-opencode/opencode-marker-distinguishes-a-shared-skills-layout
// spec: harness-opencode/coexisting-layouts-are-reported
func TestAutodetectOpenCodeWinsOverSharedSkillsLocations(t *testing.T) {
	for name, shared := range map[string]fstest.MapFS{
		"agents compatibility skills": competingLayouts["agents compatibility skills"],
		"bare skills directory":       competingLayouts["bare skills directory"],
	} {
		t.Run(name, func(t *testing.T) {
			if h, ok := ingest.Autodetect(shared); ok && h == ingest.HarnessOpenCode {
				t.Fatalf("control: %s alone was detected as opencode", name)
			}
			h, ok, ambiguous := autodetectAmbiguity(t, withLayouts(shared, openCodeMarker()))
			if !ok || h != ingest.HarnessOpenCode || !ambiguous {
				t.Fatalf("%s + .opencode = (%q, %v, ambiguous=%v), want opencode reported as ambiguous", name, h, ok, ambiguous)
			}
		})
	}
	if h, ok, ambiguous := autodetectAmbiguity(t, openCodeMarker()); !ok || h != ingest.HarnessOpenCode || ambiguous {
		t.Fatalf(".opencode alone = (%q, %v, ambiguous=%v), want opencode without a warning", h, ok, ambiguous)
	}
	for _, explicit := range []string{"claude plugin manifest", "factory plugin manifest", "cursor plugin manifest", "gemini extension manifest"} {
		h, ok, ambiguous := autodetectAmbiguity(t, withLayouts(competingLayouts[explicit], openCodeMarker()))
		if !ok || h == ingest.HarnessOpenCode || ambiguous {
			t.Fatalf("%s + .opencode = (%q, %v, ambiguous=%v), want the explicit manifest", explicit, h, ok, ambiguous)
		}
	}
	if h, ok, _ := autodetectAmbiguity(t, withLayouts(devinWorkspace(), openCodeMarker())); !ok || h != ingest.HarnessDevin {
		t.Fatalf(".devin + .opencode = (%q, %v), want devin", h, ok)
	}
}

// spec: harness-opencode/a-dependency-only-opencode-directory-is-not-a-marker
func TestAutodetectIgnoresDependencyOnlyOpenCodeDirectory(t *testing.T) {
	dependencies := fstest.MapFS{
		".opencode/package.json":       &fstest.MapFile{Data: []byte(`{"dependencies":{"@opencode-ai/plugin":"1.18.34"}}`)},
		".opencode/package-lock.json":  &fstest.MapFile{Data: []byte(`{}`)},
		".opencode/node_modules/.keep": &fstest.MapFile{Data: []byte("")},
	}
	if h, ok := ingest.Autodetect(dependencies); ok && h == ingest.HarnessOpenCode {
		t.Fatal("a dependency-only .opencode directory was detected as opencode")
	}
	h, ok, ambiguous := autodetectAmbiguity(t, withLayouts(competingLayouts["claude workspace"], dependencies))
	if !ok || h != ingest.HarnessClaude || ambiguous {
		t.Fatalf("claude workspace + dependency-only .opencode = (%q, %v, ambiguous=%v), want claude without a warning", h, ok, ambiguous)
	}
	for _, entry := range []string{"skills/demo/SKILL.md", "skill/demo/SKILL.md", "commands/check.md", "command/check.md", "agents/a.md", "agent/a.md", "plugins/p.js", "plugin/p.js", "hooks/agent-marketplace/h.json", "opencode.json", "opencode.jsonc"} {
		src := withLayouts(dependencies, fstest.MapFS{".opencode/" + entry: &fstest.MapFile{Data: []byte("{}")}})
		if h, ok := ingest.Autodetect(src); !ok || h != ingest.HarnessOpenCode {
			t.Fatalf(".opencode/%s = (%q, %v), want opencode", entry, h, ok)
		}
	}
}
