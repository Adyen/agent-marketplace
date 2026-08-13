package devin

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// --- fixtures ---

func file(data string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(data)} }

const (
	skillDoc = "---\nname: demo-skill\ndescription: A demo skill\n---\nDo the thing\n"
	agentDoc = "---\nname: demo-agent\ndescription: A demo agent\nmodel: sonnet\nallowed-tools:\n  - read\n  - exec\nmax-nesting: 3\n---\nBe an agent\n"
)

// --- detection and root resolution ---

// TestDetectRecognizesOnlyWorkspaceMarkers is the safety test behind
// "direct user roots require explicit Devin selection": Detect is what
// autodetection consults, so every direct-root shape must answer false here.
func TestDetectRecognizesOnlyWorkspaceMarkers(t *testing.T) {
	tests := []struct {
		name string
		src  fstest.MapFS
		want bool
	}{
		{"workspace skills marker", fstest.MapFS{".devin/skills/demo-skill/SKILL.md": file(skillDoc)}, true},
		{"workspace agents marker", fstest.MapFS{".devin/agents/demo-agent.md": file(agentDoc)}, true},
		{"workspace project hooks marker", fstest.MapFS{".devin/hooks.v1.json": file(`{}`)}, true},
		{"workspace mcp marker", fstest.MapFS{".devin/mcp_config.json": file(`{}`)}, true},
		{"workspace config marker", fstest.MapFS{".devin/config.json": file(`{}`)}, true},

		// The direct `~/.config/devin` shape. Every one of these is a valid
		// LoadBundle input and must never be autodetected.
		{"direct user root skills", fstest.MapFS{"skills/demo-skill/SKILL.md": file(skillDoc)}, false},
		{"direct user root agents", fstest.MapFS{"agents/demo-agent.md": file(agentDoc)}, false},
		{"direct user root config", fstest.MapFS{"config.json": file(`{"hooks":{}}`)}, false},
		{"direct user root mcp", fstest.MapFS{"mcp_config.json": file(`{}`)}, false},
		{"direct user root complete", fstest.MapFS{
			"skills/demo-skill/SKILL.md": file(skillDoc),
			"agents/demo-agent.md":       file(agentDoc),
			"config.json":                file(`{}`),
			"mcp_config.json":            file(`{}`),
		}, false},

		// Personal override files alone are not a root: this target neither
		// reads nor writes them, so there is nothing to ingest.
		{"only local overrides", fstest.MapFS{
			".devin/config.local.json":     file(`{}`),
			".devin/mcp_config.local.json": file(`{}`),
		}, false},

		// No descendant discovery.
		{"nested .devin in a subdirectory", fstest.MapFS{"sub/.devin/skills/demo-skill/SKILL.md": file(skillDoc)}, false},
		{"nested .devin inside .devin", fstest.MapFS{".devin/.devin/skills/demo-skill/SKILL.md": file(skillDoc)}, false},

		{"unrelated layout", fstest.MapFS{".claude/skills/demo-skill/SKILL.md": file(skillDoc)}, false},
		{"empty", fstest.MapFS{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.src); got != tt.want {
				t.Errorf("Detect = %t, want %t", got, tt.want)
			}
			// Devin has no permissive fallback form, so strict detection must
			// agree exactly. A divergence would make sub-bundle scanning and
			// autodetection disagree about what a Devin root is.
			if got := (&devinIngester{}).DetectStrict(tt.src); got != tt.want {
				t.Errorf("DetectStrict = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestDetectIgnoresAnEmptyResourceDirectory pins that an empty skills or agents
// directory is not a marker. A bare `mkdir .devin/skills` must not make an
// arbitrary repository look like a Devin workspace to autodetection.
func TestDetectIgnoresAnEmptyResourceDirectory(t *testing.T) {
	// fstest.MapFS synthesizes directories from file paths only, so an empty
	// directory has to come from a real filesystem.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".devin", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if Detect(os.DirFS(root)) {
		t.Fatal("Detect = true for a .devin root whose only content is an empty skills directory")
	}
}

// TestLoadBundleAcceptsTheDirectRootOnlyWhenExplicitlySelected is the other half
// of the user-root invariant: the shape autodetection refuses is still loadable,
// because reaching LoadBundle means a caller named devin.
func TestLoadBundleAcceptsTheDirectRootOnlyWhenExplicitlySelected(t *testing.T) {
	src := fstest.MapFS{"skills/demo-skill/SKILL.md": file(skillDoc)}
	if Detect(src) {
		t.Fatal("Detect must not recognize a direct user root")
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if names := resourceNames(bundle); !reflect.DeepEqual(names, []string{"demo-skill"}) {
		t.Fatalf("resources = %v, want [demo-skill]", names)
	}
}

// TestLoadBundleNeverMergesRoots pins the plan's "project and user roots are
// never merged implicitly" rule at the only place that could break it: a
// directory that carries BOTH shapes at once.
func TestLoadBundleNeverMergesRoots(t *testing.T) {
	src := fstest.MapFS{
		".devin/skills/workspace-skill/SKILL.md": file("---\nname: workspace-skill\ndescription: In the workspace root\n---\nBody\n"),
		".devin/agents/workspace-agent.md":       file("---\nname: workspace-agent\ndescription: In the workspace root\n---\nBody\n"),
		// A direct-root shape sitting alongside it. Reading these too would be
		// the implicit merge the plan forbids.
		"skills/direct-skill/SKILL.md": file("---\nname: direct-skill\ndescription: In the direct root\n---\nBody\n"),
		"agents/direct-agent.md":       file("---\nname: direct-agent\ndescription: In the direct root\n---\nBody\n"),
		"mcp_config.json":              file(`{"mcpServers":{"direct-server":{"command":"npx"}}}`),
		"hooks.v1.json":                file(`{"Stop":[{"type":"command","command":"echo direct"}]}`),
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	// Skills first, then subagents: the resource order is the fixed per-kind
	// load order, not an alphabetical sort.
	want := []string{"workspace-skill", "workspace-agent"}
	if got := resourceNames(bundle); !reflect.DeepEqual(got, want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
}

// TestLoadBundleDoesNotDiscoverNestedOrAncestorRoots covers the two adversarial
// directions the plan names: "The ingester does not walk ancestors, descendants,
// or nested `.devin` directories."
func TestLoadBundleDoesNotDiscoverNestedOrAncestorRoots(t *testing.T) {
	t.Run("descendant .devin is not a root", func(t *testing.T) {
		src := fstest.MapFS{"sub/.devin/skills/demo-skill/SKILL.md": file(skillDoc)}
		if _, err := LoadBundle(src); err == nil {
			t.Fatal("LoadBundle accepted a root whose only Devin content is in a subdirectory")
		}
	})

	t.Run("nested .devin inside .devin is not merged", func(t *testing.T) {
		src := fstest.MapFS{
			".devin/skills/outer-skill/SKILL.md":        file("---\nname: outer-skill\ndescription: Outer\n---\nBody\n"),
			".devin/.devin/skills/inner-skill/SKILL.md": file("---\nname: inner-skill\ndescription: Inner\n---\nBody\n"),
		}
		bundle, err := LoadBundle(src)
		if err != nil {
			t.Fatalf("LoadBundle: %v", err)
		}
		if got := resourceNames(bundle); !reflect.DeepEqual(got, []string{"outer-skill"}) {
			t.Fatalf("resources = %v, want [outer-skill]", got)
		}
	})

	t.Run("ancestor .devin is unreachable", func(t *testing.T) {
		// The real-world shape: a repository root with .devin, and the caller
		// pointing at a subdirectory. Devin's own runtime walks upward for
		// .git/.jj; this ingester must not.
		parent := t.TempDir()
		writeFile(t, filepath.Join(parent, ".devin", "skills", "demo-skill", "SKILL.md"), skillDoc)
		child := filepath.Join(parent, "workspace")
		if err := os.MkdirAll(child, 0o755); err != nil {
			t.Fatal(err)
		}
		src := os.DirFS(child)
		if Detect(src) {
			t.Fatal("Detect walked up to the parent .devin")
		}
		if _, err := LoadBundle(src); err == nil {
			t.Fatal("LoadBundle walked up to the parent .devin")
		}
	})
}

// TestLoadBundleFollowsASymlinkedRoot pins the two symlink shapes the target
// depends on. D10 materializes skills as directory symlinks and flat subagents
// as file symlinks, so a Devin root produced by this tool is FULL of symlinks;
// an ingester that refused to follow them could not re-ingest its own output.
func TestLoadBundleFollowsASymlinkedRoot(t *testing.T) {
	t.Run("the .devin root itself is a symlink", func(t *testing.T) {
		store := t.TempDir()
		writeFile(t, filepath.Join(store, "skills", "demo-skill", "SKILL.md"), skillDoc)
		workspace := t.TempDir()
		if err := os.Symlink(store, filepath.Join(workspace, workspaceDir)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		src := os.DirFS(workspace)
		if !Detect(src) {
			t.Fatal("Detect = false for a symlinked .devin root")
		}
		assertSingleSkill(t, src, "demo-skill")
	})

	t.Run("a skill directory inside the root is a symlink", func(t *testing.T) {
		cache := t.TempDir()
		writeFile(t, filepath.Join(cache, "demo-skill", "SKILL.md"), skillDoc)
		workspace := t.TempDir()
		if err := os.MkdirAll(filepath.Join(workspace, workspaceDir, skillsDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(cache, "demo-skill"), filepath.Join(workspace, workspaceDir, skillsDir, "demo-skill")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		src := os.DirFS(workspace)
		if !Detect(src) {
			t.Fatal("Detect = false for a root whose skill is a directory symlink")
		}
		assertSingleSkill(t, src, "demo-skill")
	})
}

// --- skills and command provenance ---

func TestSkillIngest(t *testing.T) {
	tests := []struct {
		name           string
		doc            string
		wantTriggers   []string
		wantTools      []string
		wantModel      string
		wantExtra      map[string]any
		wantPermission bool
	}{
		{
			name:         "canonical skill",
			doc:          "---\nname: demo-skill\ndescription: A demo skill\ntriggers:\n  - user\n  - model\n---\nBody\n",
			wantTriggers: []string{"user", "model"},
		},
		{
			name: "absent triggers stay absent",
			doc:  "---\nname: demo-skill\ndescription: A demo skill\n---\nBody\n",
		},
		{
			// The question D4 left open: a Devin skill whose triggers were all
			// unmappable is emitted as `triggers: []`. On the way back in it is
			// an EXPLICIT EMPTY list — the narrowest activation — never nil.
			// Reading it as nil would silently widen activation back to Devin's
			// user+model default.
			name:         "explicit empty triggers stay explicitly empty",
			doc:          "---\nname: demo-skill\ndescription: A demo skill\ntriggers: []\n---\nBody\n",
			wantTriggers: []string{},
		},
		{
			name:      "devin allowed-tools reverse to canonical ids",
			doc:       "---\nname: demo-skill\ndescription: A demo skill\nallowed-tools:\n  - read\n  - exec\n  - glob\n---\nBody\n",
			wantTools: []string{"Read", "Bash", "Glob"},
		},
		{
			name:      "explicit empty allowed-tools stay explicitly empty",
			doc:       "---\nname: demo-skill\ndescription: A demo skill\nallowed-tools: []\n---\nBody\n",
			wantTools: []string{},
		},
		{
			name:      "stable devin model alias reverses",
			doc:       "---\nname: demo-skill\ndescription: A demo skill\nmodel: haiku\n---\nBody\n",
			wantModel: "haiku",
		},
		{
			name:      "unknown extension fields are preserved",
			doc:       "---\nname: demo-skill\ndescription: A demo skill\nx-team: payments\n---\nBody\n",
			wantExtra: map[string]any{"x-team": "payments"},
		},
		{
			name:           "permissions become typed IR",
			doc:            "---\nname: demo-skill\ndescription: A demo skill\npermissions:\n  allow:\n    - Read\n---\nBody\n",
			wantPermission: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := mustLoad(t, fstest.MapFS{".devin/skills/demo-skill/SKILL.md": file(tt.doc)})
			assertNoErrorDiagnostics(t, bundle)
			skill := requireSkill(t, bundle)

			if !reflect.DeepEqual(skill.Triggers, tt.wantTriggers) {
				t.Errorf("Triggers = %#v, want %#v", skill.Triggers, tt.wantTriggers)
			}
			if !reflect.DeepEqual(skill.AllowedTools, tt.wantTools) {
				t.Errorf("AllowedTools = %#v, want %#v", skill.AllowedTools, tt.wantTools)
			}
			if skill.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", skill.Model, tt.wantModel)
			}
			if skill.PermissionsSet != tt.wantPermission {
				t.Errorf("PermissionsSet = %t, want %t", skill.PermissionsSet, tt.wantPermission)
			}
			if tt.wantExtra != nil && !reflect.DeepEqual(skill.Extra, tt.wantExtra) {
				t.Errorf("Extra = %#v, want %#v", skill.Extra, tt.wantExtra)
			}
			assertNoReservedExtras(t, ir.KindSkill, skill.Extra)
		})
	}
}

// TestCommandProvenanceIsRecovered covers the plan's requirement that "The Devin
// ingester interprets that value as command provenance. An ordinary Devin skill
// remains a skill."
func TestCommandProvenanceIsRecovered(t *testing.T) {
	tests := []struct {
		name      string
		doc       string
		wantKind  ir.ResourceKind
		wantExtra map[string]any
		wantCodes []string
	}{
		{
			name:     "the marker selects a command",
			doc:      "---\nname: demo-cmd\ndescription: A demo command\nx-agent-marketplace-kind: command\ntriggers:\n  - user\n---\nRun it\n",
			wantKind: ir.KindCommand,
		},
		{
			name:      "argument metadata is restored into extras",
			doc:       "---\nname: demo-cmd\ndescription: A demo command\nx-agent-marketplace-kind: command\ntriggers:\n  - user\nargument-hint: \"<file>\"\n---\nRun it\n",
			wantKind:  ir.KindCommand,
			wantExtra: map[string]any{"argument-hint": "<file>"},
		},
		{
			name:      "the tool restriction is restored in canonical vocabulary",
			doc:       "---\nname: demo-cmd\ndescription: A demo command\nx-agent-marketplace-kind: command\ntriggers:\n  - user\nallowed-tools:\n  - read\n---\nRun it\n",
			wantKind:  ir.KindCommand,
			wantExtra: map[string]any{"allowed-tools": []string{"Read"}},
		},
		{
			// An unrecognized provenance value stays a skill AND is reported:
			// the compiler owns the key unconditionally, so it can neither be
			// interpreted nor preserved as an extension field.
			name:      "an unrelated marker value stays a skill and is reported",
			doc:       "---\nname: demo-skill\ndescription: A demo skill\nx-agent-marketplace-kind: skill\n---\nBody\n",
			wantKind:  ir.KindSkill,
			wantCodes: []string{diag.CodeIngestDevinReservedFieldDropped},
		},
		{
			name:      "a command with a non-user trigger reports the dropped field",
			doc:       "---\nname: demo-cmd\ndescription: A demo command\nx-agent-marketplace-kind: command\ntriggers:\n  - model\n---\nRun it\n",
			wantKind:  ir.KindCommand,
			wantCodes: []string{diag.CodeIngestDevinCommandFieldDropped},
		},
		{
			name:      "a command with skill-only fields reports each one",
			doc:       "---\nname: demo-cmd\ndescription: A demo command\nx-agent-marketplace-kind: command\nmodel: sonnet\nsubagent: explore\nagent: reviewer\npermissions:\n  allow:\n    - Read\n---\nRun it\n",
			wantKind:  ir.KindCommand,
			wantCodes: []string{diag.CodeIngestDevinCommandFieldDropped, diag.CodeIngestDevinCommandFieldDropped, diag.CodeIngestDevinCommandFieldDropped, diag.CodeIngestDevinCommandFieldDropped},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := mustLoad(t, fstest.MapFS{".devin/skills/demo/SKILL.md": file(tt.doc)})
			if len(bundle.Resources) != 1 {
				t.Fatalf("resources = %d, want 1", len(bundle.Resources))
			}
			resource := bundle.Resources[0]
			if resource.Kind() != tt.wantKind {
				t.Fatalf("Kind = %v, want %v", resource.Kind(), tt.wantKind)
			}
			if got := warningCodes(bundle); !reflect.DeepEqual(got, tt.wantCodes) {
				t.Errorf("warning codes = %v, want %v", got, tt.wantCodes)
			}

			var extra map[string]any
			switch typed := resource.(type) {
			case *ir.Command:
				extra = typed.Extra
			case *ir.Skill:
				extra = typed.Extra
			}
			// The provenance marker is CONSUMED. Leaving it in Extra would make
			// every recompile emit compile.canonical-field-overrides-extra for a
			// conflict the ingester itself invented.
			assertNoReservedExtras(t, resource.Kind(), extra)
			if tt.wantExtra != nil && !reflect.DeepEqual(extra, tt.wantExtra) {
				t.Errorf("Extra = %#v, want %#v", extra, tt.wantExtra)
			}
		})
	}
}

// --- subagents ---

func TestSubagentIngest(t *testing.T) {
	three := 3
	tests := []struct {
		name           string
		doc            string
		wantModel      string
		wantTools      []string
		wantMaxNesting *int
		wantCodes      []string
	}{
		{
			name:           "canonical flat profile",
			doc:            agentDoc,
			wantModel:      "sonnet",
			wantTools:      []string{"Read", "Bash"},
			wantMaxNesting: &three,
		},
		{
			// Plan, models: "Reverse ingestion leaves an absent model empty."
			name: "an absent model stays empty",
			doc:  "---\nname: demo-agent\ndescription: A demo agent\n---\nBody\n",
		},
		{
			// Devin does not read `tools`, so the restriction it declares is
			// not enforced; carrying it into canonical IR would make it look
			// enforced on the next target.
			name:      "the claude tools spelling is dropped and reported",
			doc:       "---\nname: demo-agent\ndescription: A demo agent\ntools:\n  - Read\n---\nBody\n",
			wantCodes: []string{diag.CodeIngestDevinReservedFieldDropped},
		},
		{
			name:      "allowed-tools wins over tools and reports the drop",
			doc:       "---\nname: demo-agent\ndescription: A demo agent\ntools:\n  - WebFetch\nallowed-tools:\n  - read\n---\nBody\n",
			wantTools: []string{"Read"},
			wantCodes: []string{diag.CodeIngestDevinReservedFieldDropped},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := mustLoad(t, fstest.MapFS{".devin/agents/demo-agent.md": file(tt.doc)})
			assertNoErrorDiagnostics(t, bundle)
			if len(bundle.Resources) != 1 {
				t.Fatalf("resources = %d, want 1", len(bundle.Resources))
			}
			subagent, ok := bundle.Resources[0].(*ir.Subagent)
			if !ok {
				t.Fatalf("resource type = %T, want *ir.Subagent", bundle.Resources[0])
			}
			if subagent.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", subagent.Model, tt.wantModel)
			}
			if !reflect.DeepEqual(subagent.Tools, tt.wantTools) {
				t.Errorf("Tools = %#v, want %#v", subagent.Tools, tt.wantTools)
			}
			if !reflect.DeepEqual(subagent.MaxNesting, tt.wantMaxNesting) {
				t.Errorf("MaxNesting = %v, want %v", derefInt(subagent.MaxNesting), derefInt(tt.wantMaxNesting))
			}
			if got := warningCodes(bundle); !reflect.DeepEqual(got, tt.wantCodes) {
				t.Errorf("warning codes = %v, want %v", got, tt.wantCodes)
			}
			assertNoReservedExtras(t, ir.KindSubagent, subagent.Extra)
		})
	}
}

// TestSubagentDirectoryFormIsReportedNotIngested pins the deliberate scope
// choice: Devin accepts agents/<name>/AGENT.md, this target neither writes nor
// ingests it, and the omission is reported rather than silent.
func TestSubagentDirectoryFormIsReportedNotIngested(t *testing.T) {
	bundle := mustLoad(t, fstest.MapFS{
		".devin/agents/flat-agent.md":         file("---\nname: flat-agent\ndescription: Flat\n---\nBody\n"),
		".devin/agents/dir-agent/AGENT.md":    file("---\nname: dir-agent\ndescription: Directory form\n---\nBody\n"),
		".devin/agents/dir-agent/notes.md":    file("notes\n"),
		".devin/skills/demo-skill/SKILL.md":   file(skillDoc),
		".devin/skills/demo-skill/scripts/go": file("#!/bin/sh\n"),
	})
	if got := resourceNames(bundle); !reflect.DeepEqual(got, []string{"demo-skill", "flat-agent"}) {
		t.Fatalf("resources = %v, want [demo-skill flat-agent]", got)
	}
	assertDiagnostic(t, bundle, diag.CodeIngestDevinSubagentDirectoryFormSkipped, diag.SeverityWarning, "dir-agent")
}

// --- hooks ---

func TestHookIngest(t *testing.T) {
	thirty := 30
	tests := []struct {
		name      string
		src       fstest.MapFS
		wantHooks []wantHook
		wantCodes []string
	}{
		{
			name: "project hooks.v1.json carries the hook object directly",
			src: fstest.MapFS{".devin/hooks.v1.json": file(
				`{"PreToolUse":[{"type":"command","command":"echo pre","matcher":"Read","timeout":30}]}`)},
			wantHooks: []wantHook{{"PreToolUse-command", "PreToolUse", "Read", "command", "echo pre", "", &thirty}},
		},
		{
			name: "user config.json carries hooks under the hooks member",
			src: fstest.MapFS{".devin/config.json": file(
				`{"model":"sonnet","hooks":{"SessionEnd":[{"type":"prompt","prompt":"wrap up"}]}}`)},
			wantHooks: []wantHook{{"SessionEnd-prompt", "SessionEnd", "", "prompt", "", "wrap up", nil}},
		},
		{
			name: "every documented devin event reverses",
			src: fstest.MapFS{".devin/hooks.v1.json": file(
				`{"PermissionRequest":[{"type":"command","command":"a"}],` +
					`"PostCompaction":[{"type":"command","command":"b"}],` +
					`"SessionStart":[{"type":"command","command":"c"}],` +
					`"Stop":[{"type":"command","command":"d"}],` +
					`"UserPromptSubmit":[{"type":"command","command":"e"}]}`)},
			wantHooks: []wantHook{
				{"PermissionRequest-command", "PermissionRequest", "", "command", "a", "", nil},
				{"PostCompaction-command", "PostCompaction", "", "command", "b", "", nil},
				{"SessionStart-command", "SessionStart", "", "command", "c", "", nil},
				{"Stop-command", "Stop", "", "command", "d", "", nil},
				{"UserPromptSubmit-command", "UserPromptSubmit", "", "command", "e", "", nil},
			},
		},
		{
			name: "the claude matcher-group shape is accepted",
			src: fstest.MapFS{".devin/hooks.v1.json": file(
				`{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo post"}]}]}`)},
			wantHooks: []wantHook{{"PostToolUse-command", "PostToolUse", "Edit", "command", "echo post", "", nil}},
		},
		{
			name: "an absent type defaults to command",
			src: fstest.MapFS{".devin/hooks.v1.json": file(
				`{"Stop":[{"command":"echo stop"}]}`)},
			wantHooks: []wantHook{{"Stop-command", "Stop", "", "command", "echo stop", "", nil}},
		},
		{
			name: "hooks from both canonical locations get unique names",
			src: fstest.MapFS{
				".devin/hooks.v1.json": file(`{"Stop":[{"type":"command","command":"first"}]}`),
				".devin/config.json":   file(`{"hooks":{"Stop":[{"type":"command","command":"second"}]}}`),
			},
			wantHooks: []wantHook{
				{"Stop-command", "Stop", "", "command", "first", "", nil},
				{"Stop-command-2", "Stop", "", "command", "second", "", nil},
			},
		},
		{
			name: "claude-family metadata members are not mistaken for events",
			src: fstest.MapFS{".devin/hooks.v1.json": file(
				`{"claudeHooksImported":true,"Stop":[{"type":"command","command":"echo stop"}]}`)},
			wantHooks: []wantHook{{"Stop-command", "Stop", "", "command", "echo stop", "", nil}},
		},
		{
			name: "an undocumented event is preserved and reported",
			src: fstest.MapFS{".devin/hooks.v1.json": file(
				`{"NoSuchEvent":[{"type":"command","command":"echo x"}]}`)},
			wantHooks: []wantHook{{"NoSuchEvent-command", "NoSuchEvent", "", "command", "echo x", "", nil}},
			wantCodes: []string{diag.CodeIngestDevinHookEventUnmappable},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := mustLoad(t, tt.src)
			assertNoErrorDiagnostics(t, bundle)
			var got []wantHook
			for _, resource := range bundle.Resources {
				hook, ok := resource.(*ir.Hook)
				if !ok {
					t.Fatalf("resource type = %T, want *ir.Hook", resource)
				}
				got = append(got, wantHook{hook.Name(), hook.Event, hook.Matcher, hook.Type, hook.Command, hook.Prompt, hook.Timeout})
			}
			if !reflect.DeepEqual(got, tt.wantHooks) {
				t.Fatalf("hooks = %+v, want %+v", got, tt.wantHooks)
			}
			if codes := warningCodes(bundle); !reflect.DeepEqual(codes, tt.wantCodes) {
				t.Errorf("warning codes = %v, want %v", codes, tt.wantCodes)
			}
		})
	}
}

type wantHook struct {
	Name    string
	Event   string
	Matcher string
	Type    string
	Command string
	Prompt  string
	Timeout *int
}

// --- MCP servers ---

func TestMcpIngest(t *testing.T) {
	bundle := mustLoad(t, fstest.MapFS{".devin/mcp_config.json": file(`{
	  "mcpServers": {
	    "remote": {
	      "url": "https://example.invalid/mcp",
	      "transport": "http",
	      "headers": {"X-Client": "${env:CLIENT_ID}"},
	      "oauthClientId": "client-id",
	      "oauthClientSecret": "${env:OAUTH_CLIENT_SECRET}",
	      "oauthResource": "https://example.invalid",
	      "disabled": false,
	      "x-note": "kept"
	    },
	    "stdio": {
	      "command": "npx",
	      "args": ["-y", "demo"],
	      "env": {"TOKEN": "${env:TOKEN}"},
	      "disabled": true
	    }
	  }
	}`)})
	assertNoErrorDiagnostics(t, bundle)
	if got := resourceNames(bundle); !reflect.DeepEqual(got, []string{"remote", "stdio"}) {
		t.Fatalf("resources = %v, want [remote stdio]", got)
	}

	remote := bundle.Resources[0].(*ir.McpServer)
	if remote.URL != "https://example.invalid/mcp" || remote.Transport != "http" {
		t.Errorf("remote url/transport = %q/%q", remote.URL, remote.Transport)
	}
	// ${env:VAR} is normalized to the canonical inline form so the IR is
	// harness-neutral; compiling back to Devin restores ${env:VAR}.
	if got := remote.Headers["X-Client"]; got != "${CLIENT_ID}" {
		t.Errorf("header = %q, want ${CLIENT_ID}", got)
	}
	if remote.OAuthClientSecret != "${OAUTH_CLIENT_SECRET}" {
		t.Errorf("oauth secret = %q, want ${OAUTH_CLIENT_SECRET}", remote.OAuthClientSecret)
	}
	if remote.OAuthClientID != "client-id" || remote.OAuthResource != "https://example.invalid" {
		t.Errorf("oauth id/resource = %q/%q", remote.OAuthClientID, remote.OAuthResource)
	}
	// An explicit `disabled: false` must stay explicit: collapsing it to
	// "absent" would lose the author's statement.
	if !remote.DisabledSet || remote.Disabled {
		t.Errorf("DisabledSet/Disabled = %t/%t, want true/false", remote.DisabledSet, remote.Disabled)
	}
	if !reflect.DeepEqual(remote.Extra, map[string]any{"x-note": "kept"}) {
		t.Errorf("Extra = %#v", remote.Extra)
	}

	stdio := bundle.Resources[1].(*ir.McpServer)
	if stdio.Command != "npx" || !reflect.DeepEqual(stdio.Args, []string{"-y", "demo"}) {
		t.Errorf("stdio command/args = %q/%v", stdio.Command, stdio.Args)
	}
	if got := stdio.Env["TOKEN"]; got != "${TOKEN}" {
		t.Errorf("env = %q, want ${TOKEN}", got)
	}
	if !stdio.DisabledSet || !stdio.Disabled {
		t.Errorf("DisabledSet/Disabled = %t/%t, want true/true", stdio.DisabledSet, stdio.Disabled)
	}
	assertDiagnostic(t, bundle, diag.CodeIngestDevinMcpEnvNormalized, diag.SeverityInfo, "remote")
}

// TestLegacyMcpLocationIsNeverRead pins the deliberate decision recorded in
// loadMcpServers: the pre-3000.3 config.json#mcpServers block is reported and
// never ingested.
func TestLegacyMcpLocationIsNeverRead(t *testing.T) {
	bundle := mustLoad(t, fstest.MapFS{
		".devin/config.json":     file(`{"mcpServers":{"legacy":{"command":"npx"}}}`),
		".devin/mcp_config.json": file(`{"mcpServers":{"current":{"command":"npx"}}}`),
	})
	if got := resourceNames(bundle); !reflect.DeepEqual(got, []string{"current"}) {
		t.Fatalf("resources = %v, want [current]", got)
	}
	assertDiagnostic(t, bundle, diag.CodeIngestDevinLegacyMcpLocationIgnored, diag.SeverityWarning, "bundle")
}

// TestLocalOverridesAreReportedNotRead pins that the project-local personal
// files are neither ingested nor silently skipped.
func TestLocalOverridesAreReportedNotRead(t *testing.T) {
	bundle := mustLoad(t, fstest.MapFS{
		".devin/mcp_config.json":       file(`{"mcpServers":{"shared":{"command":"npx"}}}`),
		".devin/mcp_config.local.json": file(`{"mcpServers":{"personal":{"command":"npx"}}}`),
		".devin/config.local.json":     file(`{"hooks":{"Stop":[{"type":"command","command":"personal"}]}}`),
	})
	if got := resourceNames(bundle); !reflect.DeepEqual(got, []string{"shared"}) {
		t.Fatalf("resources = %v, want [shared]", got)
	}
	var files []string
	for _, d := range bundle.Diagnostics {
		if d.Code == diag.CodeIngestDevinLocalOverrideIgnored {
			if d.Severity != diag.SeverityInfo {
				t.Errorf("severity = %v, want info", d.Severity)
			}
			files = append(files, d.Source.File)
		}
	}
	sort.Strings(files)
	want := []string{".devin/config.local.json", ".devin/mcp_config.local.json"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("reported local overrides = %v, want %v", files, want)
	}
}

// --- fail-closed behavior ---

// TestUnreversibleToolFailsTheResource is the privilege-impact test on the way
// IN. Devin's tool vocabulary is closed, so a name outside it declares a
// restriction Devin does not enforce. Ingesting it would either fake an enforced
// restriction on the next target or blow up in the compiler; the resource is
// skipped with an error instead.
func TestUnreversibleToolFailsTheResource(t *testing.T) {
	tests := []struct {
		name     string
		src      fstest.MapFS
		resource string
	}{
		{
			name:     "skill",
			src:      fstest.MapFS{".devin/skills/demo-skill/SKILL.md": file("---\nname: demo-skill\ndescription: d\nallowed-tools:\n  - read\n  - NotATool\n---\nBody\n")},
			resource: "demo-skill",
		},
		{
			name:     "command",
			src:      fstest.MapFS{".devin/skills/demo-cmd/SKILL.md": file("---\nname: demo-cmd\ndescription: d\nx-agent-marketplace-kind: command\nallowed-tools:\n  - NotATool\n---\nBody\n")},
			resource: "demo-cmd",
		},
		{
			name:     "subagent",
			src:      fstest.MapFS{".devin/agents/demo-agent.md": file("---\nname: demo-agent\ndescription: d\nallowed-tools:\n  - NotATool\n---\nBody\n")},
			resource: "demo-agent",
		},
		{
			// The Claude scoped-specifier shape. It is not a Devin tool name,
			// so it must not survive ingestion either.
			name:     "scoped claude specifier",
			src:      fstest.MapFS{".devin/skills/demo-skill/SKILL.md": file("---\nname: demo-skill\ndescription: d\nallowed-tools:\n  - \"Bash(git push --force:*)\"\n---\nBody\n")},
			resource: "demo-skill",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := mustLoad(t, tt.src)
			if len(bundle.Resources) != 0 {
				t.Fatalf("resources = %d, want 0: an unenforceable tool restriction must not become canonical IR", len(bundle.Resources))
			}
			assertDiagnostic(t, bundle, diag.CodeIngestDevinToolUnreversible, diag.SeverityError, tt.resource)
		})
	}
}

// TestLoadFailuresAreScopedToOneResource pins the plan's per-resource
// suppression rule on the ingest side: a broken file reports itself and every
// other resource still loads.
func TestLoadFailuresAreScopedToOneResource(t *testing.T) {
	tests := []struct {
		name     string
		src      fstest.MapFS
		code     string
		resource string
		wantKept []string
	}{
		{
			name: "malformed skill frontmatter",
			src: fstest.MapFS{
				".devin/skills/broken/SKILL.md": file("---\nname: [not a string\n---\nBody\n"),
				".devin/skills/good/SKILL.md":   file("---\nname: good\ndescription: d\n---\nBody\n"),
			},
			code: diag.CodeIngestDevinLoadSkillFailed, resource: ".devin/skills/broken", wantKept: []string{"good"},
		},
		{
			name: "malformed subagent frontmatter",
			src: fstest.MapFS{
				".devin/agents/broken.md": file("---\nisolation: [1, 2]\n---\nBody\n"),
				".devin/agents/good.md":   file("---\nname: good\ndescription: d\n---\nBody\n"),
			},
			code: diag.CodeIngestDevinLoadSubagentFailed, resource: ".devin/agents/broken.md", wantKept: []string{"good"},
		},
		{
			name: "duplicate json key in the hook file",
			src: fstest.MapFS{
				".devin/hooks.v1.json":        file(`{"Stop":[{"type":"command","command":"a","command":"b"}]}`),
				".devin/skills/good/SKILL.md": file("---\nname: good\ndescription: d\n---\nBody\n"),
			},
			code: diag.CodeJSONDuplicateKey, resource: ".devin/hooks.v1.json", wantKept: []string{"good"},
		},
		{
			name: "duplicate json key in the mcp file",
			src: fstest.MapFS{
				".devin/mcp_config.json":      file(`{"mcpServers":{"a":{"command":"x"},"a":{"command":"y"}}}`),
				".devin/skills/good/SKILL.md": file("---\nname: good\ndescription: d\n---\nBody\n"),
			},
			code: diag.CodeJSONDuplicateKey, resource: ".devin/mcp_config.json", wantKept: []string{"good"},
		},
		{
			name: "malformed json in the mcp file",
			src: fstest.MapFS{
				".devin/mcp_config.json":      file(`{"mcpServers":`),
				".devin/skills/good/SKILL.md": file("---\nname: good\ndescription: d\n---\nBody\n"),
			},
			code: diag.CodeJSONMalformed, resource: ".devin/mcp_config.json", wantKept: []string{"good"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := mustLoad(t, tt.src)
			if got := resourceNames(bundle); !reflect.DeepEqual(got, tt.wantKept) {
				t.Fatalf("resources = %v, want %v", got, tt.wantKept)
			}
			assertDiagnostic(t, bundle, tt.code, diag.SeverityError, tt.resource)
		})
	}
}

// TestLoadBundleIsDeterministic runs the same rich root repeatedly and requires
// byte-identical resource order, names, and diagnostic codes. Map iteration
// order is the failure mode this guards.
func TestLoadBundleIsDeterministic(t *testing.T) {
	src := richRoot()
	var first []string
	for i := 0; i < 25; i++ {
		bundle := mustLoad(t, src)
		signature := append(resourceNames(bundle), allCodes(bundle)...)
		if i == 0 {
			first = signature
			continue
		}
		if !reflect.DeepEqual(signature, first) {
			t.Fatalf("run %d signature = %v, want %v", i, signature, first)
		}
	}
}

// TestDiagnosticsNeverQuoteValues is the ingest-side leak test. Every value the
// plan's observability section forbids in logs and diagnostics is planted in a
// root that triggers as many diagnostics as possible, and no diagnostic may
// contain any of them.
func TestDiagnosticsNeverQuoteValues(t *testing.T) {
	const (
		hookCommand  = "curl -H 'Authorization: Bearer HOOK-SECRET-VALUE'"
		promptBody   = "PROMPT-SECRET-VALUE"
		matcherValue = "MATCHER-SECRET-VALUE"
		envValue     = "ENV-SECRET-VALUE"
		headerValue  = "HEADER-SECRET-VALUE"
		oauthSecret  = "OAUTH-SECRET-VALUE"
		toolValue    = "Bash(git push --force:*)"
		permission   = "PERMISSION-SECRET-VALUE"
		body         = "BODY-SECRET-VALUE"
		legacyEnv    = "LEGACY-SECRET-VALUE"
	)
	secrets := []string{hookCommand, promptBody, matcherValue, envValue, headerValue, oauthSecret, toolValue, permission, body, legacyEnv}

	src := fstest.MapFS{
		".devin/skills/leaky-skill/SKILL.md": file("---\nname: leaky-skill\ndescription: d\nallowed-tools:\n  - \"" + toolValue + "\"\npermissions:\n  allow:\n    - " + permission + "\n---\n" + body + "\n"),
		".devin/skills/leaky-cmd/SKILL.md":   file("---\nname: leaky-cmd\ndescription: d\nx-agent-marketplace-kind: command\nmodel: swe\npermissions:\n  allow:\n    - " + permission + "\n---\n" + body + "\n"),
		".devin/agents/leaky-agent.md":       file("---\nname: leaky-agent\ndescription: d\nmodel: swe\ntools:\n  - " + permission + "\n---\n" + body + "\n"),
		".devin/agents/dir-agent/AGENT.md":   file("---\nname: dir-agent\ndescription: d\n---\n" + body + "\n"),
		".devin/hooks.v1.json": file(`{"NoSuchEvent":[{"type":"command","command":` + quote(hookCommand) +
			`,"prompt":` + quote(promptBody) + `,"matcher":` + quote(matcherValue) + `}]}`),
		".devin/config.json": file(`{"mcpServers":{"legacy":{"command":` + quote(legacyEnv) + `}},"hooks":{"Stop":[{"type":"prompt","prompt":` + quote(promptBody) + `}]}}`),
		".devin/mcp_config.json": file(`{"mcpServers":{"leaky":{"url":"https://example.invalid/mcp","headers":{"X-Token":` + quote(headerValue) +
			`},"oauthClientSecret":` + quote(oauthSecret) + `,"env":{"TOKEN":` + quote(envValue) + `}}}}`),
		".devin/config.local.json": file(`{}`),
	}

	bundle := mustLoad(t, src)
	if len(bundle.Diagnostics) == 0 {
		t.Fatal("fixture produced no diagnostics; the leak test would be vacuous")
	}
	for _, d := range bundle.Diagnostics {
		for _, secret := range secrets {
			for label, text := range map[string]string{"message": d.Message, "hint": d.Hint, "resource": d.Resource} {
				if strings.Contains(text, secret) {
					t.Errorf("diagnostic %s leaked a forbidden value in its %s: %q", d.Code, label, text)
				}
			}
		}
	}
}

func quote(value string) string { return `"` + value + `"` }

// TestIngesterIsRegistered pins the registration the target registry declares.
func TestIngesterIsRegistered(t *testing.T) {
	registered, ok := ingest.For(ingest.HarnessDevin)
	if !ok {
		t.Fatal("no ingester registered for devin")
	}
	if registered.Source() != ingest.HarnessDevin {
		t.Fatalf("Source = %q, want %q", registered.Source(), ingest.HarnessDevin)
	}
	if _, ok := registered.(ingest.StrictDetector); !ok {
		t.Fatal("the Devin ingester must implement ingest.StrictDetector")
	}
}

// --- helpers ---

func richRoot() fstest.MapFS {
	return fstest.MapFS{
		".devin/skills/demo-skill/SKILL.md":   file("---\nname: demo-skill\ndescription: d\ntriggers:\n  - user\nallowed-tools:\n  - read\n---\nBody\n"),
		".devin/skills/demo-skill/scripts/go": file("#!/bin/sh\n"),
		".devin/skills/demo-cmd/SKILL.md":     file("---\nname: demo-cmd\ndescription: d\nx-agent-marketplace-kind: command\ntriggers:\n  - user\n---\nBody\n"),
		".devin/skills/other-skill/SKILL.md":  file("---\nname: other-skill\ndescription: d\n---\nBody\n"),
		".devin/agents/demo-agent.md":         file(agentDoc),
		".devin/agents/other-agent.md":        file("---\nname: other-agent\ndescription: d\nmodel: opus\n---\nBody\n"),
		".devin/hooks.v1.json": file(`{"PreToolUse":[{"type":"command","command":"echo a","matcher":"Read"}],` +
			`"Stop":[{"type":"prompt","prompt":"wrap up"},{"type":"command","command":"echo b"}]}`),
		".devin/mcp_config.json": file(`{"mcpServers":{"alpha":{"command":"npx","args":["-y","a"],"env":{"T":"${env:T}"}},` +
			`"beta":{"url":"https://example.invalid/mcp","transport":"http"}}}`),
	}
}

func mustLoad(t *testing.T, src fstest.MapFS) *ingest.Bundle {
	t.Helper()
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	return bundle
}

func requireSkill(t *testing.T, bundle *ingest.Bundle) *ir.Skill {
	t.Helper()
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(bundle.Resources))
	}
	skill, ok := bundle.Resources[0].(*ir.Skill)
	if !ok {
		t.Fatalf("resource type = %T, want *ir.Skill", bundle.Resources[0])
	}
	return skill
}

func resourceNames(bundle *ingest.Bundle) []string {
	names := make([]string, 0, len(bundle.Resources))
	for _, resource := range bundle.Resources {
		names = append(names, resource.Name())
	}
	return names
}

func allCodes(bundle *ingest.Bundle) []string {
	codes := make([]string, 0, len(bundle.Diagnostics))
	for _, d := range bundle.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

// warningCodes returns the codes of every non-info diagnostic except the
// always-present synthesized-manifest note, so a table can state exactly which
// losses a fixture is expected to report.
func warningCodes(bundle *ingest.Bundle) []string {
	var codes []string
	for _, d := range bundle.Diagnostics {
		if d.Severity == diag.SeverityInfo {
			continue
		}
		codes = append(codes, d.Code)
	}
	sort.Strings(codes)
	if len(codes) == 0 {
		return nil
	}
	return codes
}

func assertNoErrorDiagnostics(t *testing.T, bundle *ingest.Bundle) {
	t.Helper()
	for _, d := range bundle.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected error diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

func assertDiagnostic(t *testing.T, bundle *ingest.Bundle, code string, severity diag.Severity, resource string) {
	t.Helper()
	for _, d := range bundle.Diagnostics {
		if d.Code != code {
			continue
		}
		if d.Severity != severity {
			t.Errorf("diagnostic %s severity = %v, want %v", code, d.Severity, severity)
		}
		if d.Resource != resource {
			t.Errorf("diagnostic %s resource = %q, want %q", code, d.Resource, resource)
		}
		return
	}
	t.Fatalf("no %s diagnostic; got %v", code, allCodes(bundle))
}

// assertNoReservedExtras is the guard behind the D4/D7 round-trip caveat: a key
// the Devin compiler reserves must never come back in Extra, or every recompile
// emits a spurious compile.canonical-field-overrides-extra warning.
func assertNoReservedExtras(t *testing.T, kind ir.ResourceKind, extra map[string]any) {
	t.Helper()
	reserved := []string{
		kindExtensionKey, triggersKey, permissionsKey, modelKey, allowedToolsKey, toolsKey,
		argumentHintKey, subagentKey, agentKey, maxNestingKey, altMaxNestingKey,
		"disallowedTools", "isolation", "effort",
	}
	for _, key := range reserved {
		if _, present := extra[key]; !present {
			continue
		}
		// argument-hint and allowed-tools are the two deliberate exceptions:
		// ir.Command has no typed field for them, and the Devin command
		// compiler reads both FROM Extra, so putting them there is the exact
		// inverse of compilation rather than a conflict. A Skill DOES have
		// typed fields for both, so the exception must not apply there.
		if kind == ir.KindCommand && (key == argumentHintKey || key == allowedToolsKey) {
			continue
		}
		t.Errorf("Extra retained the compiler-reserved key %q; a recompile would emit a spurious canonical-field-overrides-extra warning", key)
	}
}

func assertSingleSkill(t *testing.T, src fs.FS, name string) {
	t.Helper()
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if got := resourceNames(bundle); !reflect.DeepEqual(got, []string{name}) {
		t.Fatalf("resources = %v, want [%s]", got, name)
	}
}

func derefInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
