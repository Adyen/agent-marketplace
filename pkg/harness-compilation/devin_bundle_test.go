package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// devinSkillCommandFS is a Claude-plugin bundle with one skill and one command
// whose names do not collide.
var devinSkillCommandFS = fstest.MapFS{
	"skills/deploy/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: deploy\ndescription: Deploy the service\ntriggers:\n  - user\n  - model\n---\nDeploy body\n"),
	},
	"commands/release.md": &fstest.MapFile{
		Data: []byte("---\nname: release\ndescription: Cut a release\n---\nRelease body\n"),
	},
}

// devinCollisionFS is the cross-kind collision fixture: a skill and a command
// with the same name both target skills/review/SKILL.md on Devin.
var devinCollisionFS = fstest.MapFS{
	"skills/review/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: review\ndescription: Review skill\n---\nSkill body\n"),
	},
	"commands/review.md": &fstest.MapFile{
		Data: []byte("---\nname: review\ndescription: Review command\n---\nCommand body\n"),
	},
	"skills/unrelated/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: unrelated\ndescription: Unrelated skill\n---\nOther body\n"),
	},
}

// TestDevinBundleEmitsSkillsAndCommands pins the D4 bundle contract: skills and
// converted commands both land in the canonical Devin skill-directory shape,
// and the command carries the stable provenance marker with user-only
// activation.
func TestDevinBundleEmitsSkillsAndCommands(t *testing.T) {
	result, err := CompileBundle(devinSkillCommandFS, TargetDevin)
	if err != nil {
		t.Fatalf("CompileBundle(devin): %v", err)
	}
	if diag.HasErrors(result.Diagnostics) {
		t.Fatalf("unexpected error diagnostics: %+v", result.Diagnostics)
	}

	want := map[string]string{
		"skills/deploy/SKILL.md":  "---\nname: deploy\ndescription: Deploy the service\ntriggers:\n  - user\n  - model\n---\nDeploy body\n",
		"skills/release/SKILL.md": "---\nname: release\ndescription: Cut a release\nx-agent-marketplace-kind: command\ntriggers:\n  - user\n---\nRelease body\n",
	}
	if len(result.Artifacts) != len(want) {
		t.Fatalf("artifacts = %d, want %d: %+v", len(result.Artifacts), len(want), artifactPaths(result.Artifacts))
	}
	for _, a := range result.Artifacts {
		expected, ok := want[a.Path]
		if !ok {
			t.Errorf("unexpected artifact path %q", a.Path)
			continue
		}
		if got := string(a.Bytes); got != expected {
			t.Errorf("artifact %q =\n%s\nwant\n%s", a.Path, got, expected)
		}
	}
}

// TestDevinBundleSameNameSkillAndCommandCollide covers the plan's bundle
// collision rule: "A skill and converted command with the same name target one
// path. Bundle compilation emits a stable error naming both resources; neither
// silently wins."
//
// Suppression is scoped to the two colliding resources. The unrelated skill in
// the same bundle keeps its artifact, because the plan's error behavior is
// "Resources with error diagnostics are skipped, valid resources continue".
func TestDevinBundleSameNameSkillAndCommandCollide(t *testing.T) {
	result, err := CompileBundle(devinCollisionFS, TargetDevin)
	if err != nil {
		t.Fatalf("CompileBundle(devin): %v", err)
	}
	if got, want := artifactPaths(result.Artifacts), []string{"skills/unrelated/SKILL.md"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("artifacts = %v, want %v", got, want)
	}
	if !result.HasErrors() {
		t.Fatal("HasErrors() = false, want true: a collision must make the run unclean")
	}
	wantSkipped := []SkippedResource{
		{Kind: KindSkill, Name: "review", Codes: []string{diag.CodeBundleConflictCrossKindArtifactPath}},
		{Kind: KindCommand, Name: "review", Codes: []string{diag.CodeBundleConflictCrossKindArtifactPath}},
	}
	assertSkippedResources(t, result.Skipped, wantSkipped)

	var found int
	for _, d := range result.Diagnostics {
		if d.Code != diag.CodeBundleConflictCrossKindArtifactPath {
			continue
		}
		found++
		if d.Severity != SeverityError {
			t.Errorf("severity = %v, want %v", d.Severity, SeverityError)
		}
		if d.Target != string(TargetDevin) {
			t.Errorf("target = %q, want %q", d.Target, TargetDevin)
		}
		for _, fragment := range []string{`skill "review"`, `command "review"`, `"skills/review/SKILL.md"`} {
			if !strings.Contains(d.Message, fragment) {
				t.Errorf("message %q does not name %s", d.Message, fragment)
			}
		}
	}
	if found != 1 {
		t.Fatalf("cross-kind conflict diagnostics = %d, want 1", found)
	}
}

// TestDevinBundleCollisionDoesNotCollideOnOtherTargets guards the collision
// guard itself: Claude keeps commands in their own directory, so the same
// bundle must compile cleanly there.
func TestDevinBundleCollisionDoesNotCollideOnOtherTargets(t *testing.T) {
	result, err := CompileBundle(devinCollisionFS, TargetClaude)
	if err != nil {
		t.Fatalf("CompileBundle(claude): %v", err)
	}
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeBundleConflictCrossKindArtifactPath {
			t.Fatalf("unexpected cross-kind conflict on Claude: %+v", d)
		}
	}
}

// TestResolveArtifactPathConflicts asserts the guard at its own layer: only the
// resources that actually collide are dropped, and unrelated artifacts and
// fragments survive.
//
// A collision suppresses the colliding resources WHOLE, fragments included: a
// resource that keeps its aggregate entry while losing the file that entry
// describes would leave the aggregate advertising output that was never
// written.
func TestResolveArtifactPathConflicts(t *testing.T) {
	skillArtifact := Artifact{Path: "skills/review/SKILL.md", Bytes: []byte("skill")}
	commandArtifact := Artifact{Path: "skills/review/SKILL.md", Bytes: []byte("command")}
	otherArtifact := Artifact{Path: "skills/other/SKILL.md", Bytes: []byte("other")}
	fragment := Artifact{Path: "mcp_config.json", Bytes: []byte("{}"), Fragment: true, FragmentKey: "mcp_config.json"}

	tests := []struct {
		name        string
		outputs     []resourceOutput
		wantPaths   []string
		wantCodes   []string
		wantSkipped []SkippedResource
	}{
		{
			name: "cross-kind collision drops both resources",
			outputs: []resourceOutput{
				{kind: ir.KindSkill, name: "review", artifacts: []Artifact{skillArtifact}},
				{kind: ir.KindCommand, name: "review", artifacts: []Artifact{commandArtifact}},
				{kind: ir.KindSkill, name: "other", artifacts: []Artifact{otherArtifact}},
			},
			wantPaths: []string{"skills/other/SKILL.md"},
			wantCodes: []string{diag.CodeBundleConflictCrossKindArtifactPath},
			wantSkipped: []SkippedResource{
				{Kind: ir.KindSkill, Name: "review", Codes: []string{diag.CodeBundleConflictCrossKindArtifactPath}},
				{Kind: ir.KindCommand, Name: "review", Codes: []string{diag.CodeBundleConflictCrossKindArtifactPath}},
			},
		},
		{
			name: "same-kind duplicate keeps the existing duplicate-path code and drops both",
			outputs: []resourceOutput{
				{kind: ir.KindSkill, name: "review", artifacts: []Artifact{skillArtifact}},
				{kind: ir.KindSkill, name: "review-copy", artifacts: []Artifact{commandArtifact}},
			},
			wantCodes: []string{diag.CodeBundleConflictDuplicateArtifactPath},
			wantSkipped: []SkippedResource{
				{Kind: ir.KindSkill, Name: "review", Codes: []string{diag.CodeBundleConflictDuplicateArtifactPath}},
				{Kind: ir.KindSkill, Name: "review-copy", Codes: []string{diag.CodeBundleConflictDuplicateArtifactPath}},
			},
		},
		{
			name: "a conflicting resource also loses its aggregate fragment",
			outputs: []resourceOutput{
				{kind: ir.KindSkill, name: "review", artifacts: []Artifact{skillArtifact, fragment}},
				{kind: ir.KindSkill, name: "review-copy", artifacts: []Artifact{commandArtifact}},
				{kind: ir.KindMcpServer, name: "survivor", artifacts: []Artifact{fragment}},
			},
			wantPaths: []string{"mcp_config.json"},
			wantCodes: []string{diag.CodeBundleConflictDuplicateArtifactPath},
			wantSkipped: []SkippedResource{
				{Kind: ir.KindSkill, Name: "review", Codes: []string{diag.CodeBundleConflictDuplicateArtifactPath}},
				{Kind: ir.KindSkill, Name: "review-copy", Codes: []string{diag.CodeBundleConflictDuplicateArtifactPath}},
			},
		},
		{
			name: "fragments never conflict",
			outputs: []resourceOutput{
				{kind: ir.KindMcpServer, name: "a", artifacts: []Artifact{fragment}},
				{kind: ir.KindMcpServer, name: "b", artifacts: []Artifact{fragment}},
			},
			wantPaths: []string{"mcp_config.json", "mcp_config.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifacts, diagnostics, skipped := resolveArtifactPathConflicts(tt.outputs, TargetDevin)
			gotPaths := artifactPaths(artifacts)
			if len(gotPaths) != len(tt.wantPaths) {
				t.Fatalf("artifact paths = %v, want %v", gotPaths, tt.wantPaths)
			}
			for i := range tt.wantPaths {
				if gotPaths[i] != tt.wantPaths[i] {
					t.Errorf("artifact path[%d] = %q, want %q", i, gotPaths[i], tt.wantPaths[i])
				}
			}
			if len(diagnostics) != len(tt.wantCodes) {
				t.Fatalf("diagnostics = %+v, want codes %v", diagnostics, tt.wantCodes)
			}
			for i := range tt.wantCodes {
				if diagnostics[i].Code != tt.wantCodes[i] {
					t.Errorf("diagnostic[%d] code = %q, want %q", i, diagnostics[i].Code, tt.wantCodes[i])
				}
				if diagnostics[i].Severity != SeverityError {
					t.Errorf("diagnostic[%d] severity = %v, want %v", i, diagnostics[i].Severity, SeverityError)
				}
			}
			assertSkippedResources(t, skipped, tt.wantSkipped)
		})
	}
}

// assertSkippedResources compares a skip list exactly: same length, same order,
// same kinds, names, and diagnostic codes.
func assertSkippedResources(t *testing.T, got, want []SkippedResource) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("skipped = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Kind != want[i].Kind || got[i].Name != want[i].Name {
			t.Errorf("skipped[%d] = %s %q, want %s %q", i, got[i].Kind, got[i].Name, want[i].Kind, want[i].Name)
		}
		if len(got[i].Codes) != len(want[i].Codes) {
			t.Errorf("skipped[%d] codes = %v, want %v", i, got[i].Codes, want[i].Codes)
			continue
		}
		for j := range want[i].Codes {
			if got[i].Codes[j] != want[i].Codes[j] {
				t.Errorf("skipped[%d] code[%d] = %q, want %q", i, j, got[i].Codes[j], want[i].Codes[j])
			}
		}
	}
}

// TestDevinBundleUnrepresentableToolsPublishTheSkill replaces
// TestDevinBundleToolFailureSuppressesArtifact, which asserted through the
// bundle path that an unmappable allowed tool produced no artifact and an error.
//
// Bead agent-marketplace-pu6b reversed that: the skill is published with the
// allowed-tools key omitted and a warning. Asserting it at the bundle level
// matters more than at the compiler level, because the bundle result is what
// decides `sync`'s exit code — the whole reason the old contract was a problem
// was that three openspec skills made `sync` exit non-zero and install nothing.
//
// Both spellings the real skills use are covered: a scoped Bash specifier and a
// tool name with no Devin equivalent.
func TestDevinBundleUnrepresentableToolsPublishTheSkill(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
	}{
		{
			name:        "unmappable tool name",
			frontmatter: "---\nname: scan\ndescription: Scan the web\nallowed-tools:\n  - WebSearch\n---\nBody\n",
		},
		{
			// The openspec shape: `allowed-tools: Bash(openspec:*)`.
			name:        "openspec-style scoped Bash specifier",
			frontmatter: "---\nname: scan\ndescription: Scan the web\nallowed-tools: Bash(openspec:*)\n---\nBody\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fstest.MapFS{
				"skills/scan/SKILL.md": &fstest.MapFile{Data: []byte(tt.frontmatter)},
			}
			result, err := CompileBundle(src, TargetDevin)
			if err != nil {
				t.Fatalf("CompileBundle(devin): %v", err)
			}
			if diag.HasErrors(result.Diagnostics) {
				t.Fatalf("diagnostics contain an error, so sync would exit non-zero: %+v", result.Diagnostics)
			}
			if len(result.Skipped) != 0 {
				t.Fatalf("skipped = %+v, want none", result.Skipped)
			}
			if len(result.Artifacts) == 0 {
				t.Fatal("artifacts = none, want the published skill")
			}
			var skill *Artifact
			for i := range result.Artifacts {
				if result.Artifacts[i].Path == "skills/scan/SKILL.md" {
					skill = &result.Artifacts[i]
				}
			}
			if skill == nil {
				t.Fatalf("artifacts = %v, want skills/scan/SKILL.md", artifactPaths(result.Artifacts))
			}
			if strings.Contains(string(skill.Bytes), "allowed-tools") {
				t.Errorf("published skill carries an allowed-tools key:\n%s", skill.Bytes)
			}
			found := false
			for _, d := range result.Diagnostics {
				if d.Code == diag.CodeSkillDevinToolUnknown && d.Severity == SeverityWarning && d.Resource == "scan" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s warning diagnostic: %+v", diag.CodeSkillDevinToolUnknown, result.Diagnostics)
			}
		})
	}
}

// TestDevinBundleEveryKindCompiles replaces
// TestDevinBundleDeferredKindsStayDeferred. D7 landed the last deferred kind
// and deleted the transient `compile.devin.resource-pending` scaffold, so every
// marketplace-selectable kind must now produce its canonical artifact and no
// diagnostic may carry the removed code.
func TestDevinBundleEveryKindCompiles(t *testing.T) {
	result, err := compileBundleFromIR(&ingest.Bundle{
		Resources: []ir.Resource{
			ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, ir.SourceRef{}),
			ir.NewCommand("demo-cmd", "Demo command", "Body\n", nil, ir.SourceRef{}),
			ir.NewSubagent("demo-agent", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}),
			ir.NewHook("demo-hook", "PreToolUse", "echo demo", "command", nil, ir.SourceRef{}),
			ir.NewMcpServer("demo-server", "node", []string{"server.js"}, nil, nil, ir.SourceRef{}),
		},
	}, TargetDevin)
	if err != nil {
		t.Fatalf("compileBundleFromIR(devin): %v", err)
	}
	if diag.HasErrors(result.Diagnostics) {
		t.Fatalf("unexpected error diagnostics: %+v", result.Diagnostics)
	}
	for _, d := range result.Diagnostics {
		if d.Code == "compile.devin.resource-pending" {
			t.Errorf("resource %q still reports the removed transient scaffold code", d.Resource)
		}
	}
	want := map[string]bool{
		"skills/demo/SKILL.md":     true,
		"skills/demo-cmd/SKILL.md": true,
		"agents/demo-agent.md":     true,
		"hooks.v1.json":            true,
		"mcp_config.json":          true,
	}
	if len(result.Artifacts) != len(want) {
		t.Errorf("artifacts = %v, want exactly %d", artifactPaths(result.Artifacts), len(want))
	}
	got := map[string]bool{}
	for _, a := range result.Artifacts {
		got[a.Path] = true
	}
	for path := range want {
		if !got[path] {
			t.Errorf("missing artifact %q; got %v", path, artifactPaths(result.Artifacts))
		}
	}
}

// TestDevinBundleEmitsFlatSubagentProfiles pins the D5 bundle contract: a
// subagent lands in the canonical flat profile path, and a skill and a subagent
// with the same name coexist because Devin keeps them in different directories.
//
// Plan, resource mapping: "Subagent | agents/<name>.md | Native flat
// custom-subagent profile".
func TestDevinBundleEmitsFlatSubagentProfiles(t *testing.T) {
	src := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte("---\nname: reviewer\ndescription: Review changes\nmodel: opus\ntools:\n  - Read\n  - Grep\nmax-nesting: 1\n---\nReview body\n"),
		},
		"skills/reviewer/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: reviewer\ndescription: Review skill\n---\nSkill body\n"),
		},
	}
	result, err := CompileBundle(src, TargetDevin)
	if err != nil {
		t.Fatalf("CompileBundle(devin): %v", err)
	}
	if diag.HasErrors(result.Diagnostics) {
		t.Fatalf("unexpected error diagnostics: %+v", result.Diagnostics)
	}

	want := map[string]string{
		"agents/reviewer.md":       "---\nname: reviewer\ndescription: Review changes\nmodel: opus\nallowed-tools:\n  - read\n  - grep\nmax-nesting: 1\n---\nReview body\n",
		"skills/reviewer/SKILL.md": "---\nname: reviewer\ndescription: Review skill\n---\nSkill body\n",
	}
	if len(result.Artifacts) != len(want) {
		t.Fatalf("artifacts = %d, want %d: %+v", len(result.Artifacts), len(want), artifactPaths(result.Artifacts))
	}
	for _, a := range result.Artifacts {
		expected, ok := want[a.Path]
		if !ok {
			t.Errorf("unexpected artifact path %q", a.Path)
			continue
		}
		if got := string(a.Bytes); got != expected {
			t.Errorf("artifact %q =\n%s\nwant\n%s", a.Path, got, expected)
		}
	}
}

// TestDevinBundleSubagentFailuresSuppressArtifacts checks the promoted subagent
// loss cases end to end through the bundle path. Each case asserts the exact
// code, severity, and resource, plus that the failing resource — the only
// resource in the bundle — publishes no artifact.
func TestDevinBundleSubagentFailuresSuppressArtifacts(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  string
		wantCode     string
		wantResource string
	}{
		{
			name:         "isolation",
			frontmatter:  "---\nname: reviewer\ndescription: Review\nmodel: sonnet\nisolation: worktree\n---\nBody\n",
			wantCode:     diag.CodeSubagentSecurityRestrictionUnsupported,
			wantResource: "reviewer",
		},
		{
			name:         "disallowed tools",
			frontmatter:  "---\nname: reviewer\ndescription: Review\nmodel: sonnet\ndisallowedTools:\n  - Bash\n---\nBody\n",
			wantCode:     diag.CodeSubagentSecurityRestrictionUnsupported,
			wantResource: "reviewer",
		},
		// There used to be a third case here, "unmappable tool", asserting that
		// `tools: [WebSearch]` produced no artifact and an error. Bead
		// agent-marketplace-pu6b reversed that outcome, so the case moved to
		// TestDevinBundleUnrepresentableSubagentToolsPublishTheProfile with the
		// opposite assertion rather than being quietly dropped. Nothing takes its
		// slot here: the built-in-name and nested-name errors cannot be reached
		// through a bundle at all, because ir.ValidateName's kebab-case gate
		// rejects `subagent_explore` and `team/reviewer` before the compiler runs
		// (compile/devin/subagent_test.go covers both directly).
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fstest.MapFS{
				"agents/reviewer.md": &fstest.MapFile{Data: []byte(tt.frontmatter)},
			}
			result, err := CompileBundle(src, TargetDevin)
			if err != nil {
				t.Fatalf("CompileBundle(devin): %v", err)
			}
			if len(result.Artifacts) != 0 {
				t.Fatalf("artifacts = %v, want none", artifactPaths(result.Artifacts))
			}
			found := false
			for _, d := range result.Diagnostics {
				if d.Code == tt.wantCode && d.Severity == SeverityError && d.Resource == tt.wantResource && d.Target == string(TargetDevin) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s error diagnostic for %q: %+v", tt.wantCode, tt.wantResource, result.Diagnostics)
			}
		})
	}
}

// TestDevinBundleUnrepresentableSubagentToolsPublishTheProfile is the subagent
// counterpart of TestDevinBundleUnrepresentableToolsPublishTheSkill, and the
// bundle-level record that bead agent-marketplace-pu6b applied to ALL Devin
// resource kinds rather than only to the skills that motivated it.
func TestDevinBundleUnrepresentableSubagentToolsPublishTheProfile(t *testing.T) {
	src := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte("---\nname: reviewer\ndescription: Review\nmodel: sonnet\ntools:\n  - Bash(openspec:*)\n---\nBody\n"),
		},
	}
	result, err := CompileBundle(src, TargetDevin)
	if err != nil {
		t.Fatalf("CompileBundle(devin): %v", err)
	}
	if diag.HasErrors(result.Diagnostics) {
		t.Fatalf("diagnostics contain an error, so sync would exit non-zero: %+v", result.Diagnostics)
	}
	var profile *Artifact
	for i := range result.Artifacts {
		if result.Artifacts[i].Path == "agents/reviewer.md" {
			profile = &result.Artifacts[i]
		}
	}
	if profile == nil {
		t.Fatalf("artifacts = %v, want agents/reviewer.md", artifactPaths(result.Artifacts))
	}
	if strings.Contains(string(profile.Bytes), "allowed-tools") {
		t.Errorf("published profile carries an allowed-tools key:\n%s", profile.Bytes)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeSubagentDevinToolUnknown && d.Severity == SeverityWarning && d.Resource == "reviewer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing %s warning diagnostic: %+v", diag.CodeSubagentDevinToolUnknown, result.Diagnostics)
	}
}

// devinHook is a small IR hook builder for the bundle-level hook tests. Hooks
// are driven from IR rather than a Claude source layout because the layout
// loader derives hook names from the event and would make it impossible to
// express "two differently named hooks with one behavior", which is precisely
// the deduplication case under test.
func devinHook(name, event, matcher, command string, options ...ir.HookOption) *ir.Hook {
	source := ir.SourceRef{File: "hooks/hooks.json", Line: 1}
	if matcher != "" {
		return ir.NewHookWithMatcherOptions(name, event, matcher, command, "command", nil, source, options...)
	}
	return ir.NewHookWithOptions(name, event, command, "command", nil, source, options...)
}

// TestDevinBundleMergesHookFragments pins the D6 bundle contract: every
// selected hook contributes one entry to one shared aggregate file, grouped by
// Devin event name, and the merged bytes are exact.
//
// Plan, resource mapping: "Hook | Project: `hooks.v1.json`; user:
// `config.json#hooks` | Command or prompt hook merged into a shared file".
func TestDevinBundleMergesHookFragments(t *testing.T) {
	timeout := 30
	resources := []ir.Resource{
		devinHook("pre-read", "PreToolUse", "Read", "read-guard.sh"),
		devinHook("pre-edit", "PreToolUse", "Edit", "edit-guard.sh"),
		devinHook("session-start", "SessionStart", "", "seed.sh", ir.WithHookTimeoutPtr(&timeout)),
		ir.NewHookWithOptions("submit", "UserPromptSubmit", "", "prompt", nil,
			ir.SourceRef{File: "hooks/hooks.json", Line: 1}, ir.WithHookPrompt("Summarize the diff")),
	}

	tests := []struct {
		name      string
		options   []Option
		wantPath  string
		wantBytes string
	}{
		{
			name:     "project scope writes hooks.v1.json with the hook object directly",
			wantPath: "hooks.v1.json",
			wantBytes: `{
  "PreToolUse": [
    {
      "command": "read-guard.sh",
      "matcher": "Read",
      "type": "command"
    },
    {
      "command": "edit-guard.sh",
      "matcher": "Edit",
      "type": "command"
    }
  ],
  "SessionStart": [
    {
      "command": "seed.sh",
      "timeout": 30,
      "type": "command"
    }
  ],
  "UserPromptSubmit": [
    {
      "prompt": "Summarize the diff",
      "type": "prompt"
    }
  ]
}`,
		},
		{
			name:     "user scope writes the hooks member of config.json",
			options:  []Option{WithScope(ScopeUser)},
			wantPath: "config.json",
			wantBytes: `{
  "hooks": {
    "PreToolUse": [
      {
        "command": "read-guard.sh",
        "matcher": "Read",
        "type": "command"
      },
      {
        "command": "edit-guard.sh",
        "matcher": "Edit",
        "type": "command"
      }
    ],
    "SessionStart": [
      {
        "command": "seed.sh",
        "timeout": 30,
        "type": "command"
      }
    ],
    "UserPromptSubmit": [
      {
        "prompt": "Summarize the diff",
        "type": "prompt"
      }
    ]
  }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compileBundleFromIR(&ingest.Bundle{Resources: resources}, TargetDevin, tt.options...)
			if err != nil {
				t.Fatalf("compileBundleFromIR(devin): %v", err)
			}
			if diag.HasErrors(result.Diagnostics) {
				t.Fatalf("unexpected error diagnostics: %+v", result.Diagnostics)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %v, want exactly one merged aggregate", artifactPaths(result.Artifacts))
			}
			if got := result.Artifacts[0].Path; got != tt.wantPath {
				t.Fatalf("artifact path = %q, want %q", got, tt.wantPath)
			}
			if got := string(result.Artifacts[0].Bytes); got != tt.wantBytes {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, tt.wantBytes)
			}
		})
	}
}

// TestDevinBundleCollapsesDuplicateHookBehaviorTuples covers the plan's
// "Duplicate desired hook values collapse before patching" rule.
//
// The behavior identity is the Devin event name plus the digest of the
// canonical rule value (see hookBehaviorIdentity in compile/devin/hook.go).
// Hooks that differ only outside the emitted rule — here, only in their IR
// name — are one behavior and must collapse; hooks that differ in the rule
// value must not.
//
// The permutation loop is the determinism assertion: the collapsed result must
// not depend on the order the identical copies arrived in.
func TestDevinBundleCollapsesDuplicateHookBehaviorTuples(t *testing.T) {
	timeoutA := 30
	timeoutB := 60

	tests := []struct {
		name      string
		resources []ir.Resource
		wantBytes string
	}{
		{
			name: "hooks differing only in name collapse to one entry",
			resources: []ir.Resource{
				devinHook("from-registry-a", "PreToolUse", "Read", "guard.sh"),
				devinHook("from-registry-b", "PreToolUse", "Read", "guard.sh"),
				devinHook("from-registry-c", "PreToolUse", "Read", "guard.sh"),
			},
			wantBytes: `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "type": "command"
    }
  ]
}`,
		},
		{
			name: "a different matcher is a different behavior",
			resources: []ir.Resource{
				devinHook("a", "PreToolUse", "Read", "guard.sh"),
				devinHook("b", "PreToolUse", "Edit", "guard.sh"),
			},
			wantBytes: `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "type": "command"
    },
    {
      "command": "guard.sh",
      "matcher": "Edit",
      "type": "command"
    }
  ]
}`,
		},
		{
			name: "a different payload is a different behavior",
			resources: []ir.Resource{
				devinHook("a", "PreToolUse", "Read", "guard.sh"),
				devinHook("b", "PreToolUse", "Read", "other.sh"),
			},
			wantBytes: `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "type": "command"
    },
    {
      "command": "other.sh",
      "matcher": "Read",
      "type": "command"
    }
  ]
}`,
		},
		{
			name: "a different timeout is a different behavior",
			resources: []ir.Resource{
				devinHook("a", "PreToolUse", "Read", "guard.sh", ir.WithHookTimeoutPtr(&timeoutA)),
				devinHook("b", "PreToolUse", "Read", "guard.sh", ir.WithHookTimeoutPtr(&timeoutB)),
			},
			wantBytes: `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "timeout": 30,
      "type": "command"
    },
    {
      "command": "guard.sh",
      "matcher": "Read",
      "timeout": 60,
      "type": "command"
    }
  ]
}`,
		},
		{
			name: "an absent timeout is not the same behavior as an explicit zero",
			resources: []ir.Resource{
				devinHook("a", "PreToolUse", "Read", "guard.sh"),
				devinHook("b", "PreToolUse", "Read", "guard.sh", ir.WithHookTimeout(0)),
			},
			wantBytes: `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "type": "command"
    },
    {
      "command": "guard.sh",
      "matcher": "Read",
      "timeout": 0,
      "type": "command"
    }
  ]
}`,
		},
		{
			name: "a different extension field is a different behavior",
			resources: []ir.Resource{
				ir.NewHookWithMatcher("a", "PreToolUse", "Read", "guard.sh", "command", map[string]any{"showOutput": true}, ir.SourceRef{}),
				ir.NewHookWithMatcher("b", "PreToolUse", "Read", "guard.sh", "command", map[string]any{"showOutput": false}, ir.SourceRef{}),
			},
			wantBytes: `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "showOutput": true,
      "type": "command"
    },
    {
      "command": "guard.sh",
      "matcher": "Read",
      "showOutput": false,
      "type": "command"
    }
  ]
}`,
		},
		{
			name: "two canonical events that map to one Devin event collapse",
			resources: []ir.Resource{
				devinHook("a", "SessionStart", "", "seed.sh"),
				devinHook("b", "SessionStart", "", "seed.sh"),
			},
			wantBytes: `{
  "SessionStart": [
    {
      "command": "seed.sh",
      "type": "command"
    }
  ]
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compileBundleFromIR(&ingest.Bundle{Resources: tt.resources}, TargetDevin)
			if err != nil {
				t.Fatalf("compileBundleFromIR(devin): %v", err)
			}
			if diag.HasErrors(result.Diagnostics) {
				t.Fatalf("unexpected error diagnostics: %+v", result.Diagnostics)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %v, want exactly one merged aggregate", artifactPaths(result.Artifacts))
			}
			if got := string(result.Artifacts[0].Bytes); got != tt.wantBytes {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, tt.wantBytes)
			}
		})
	}
}

// TestDevinBundleDuplicateCollapseIsOrderIndependent asserts the collapse is
// stable: inserting extra identical copies anywhere in the resource list must
// not change the merged bytes.
func TestDevinBundleDuplicateCollapseIsOrderIndependent(t *testing.T) {
	first := devinHook("first", "PreToolUse", "Read", "guard.sh")
	second := devinHook("second", "PreToolUse", "Edit", "other.sh")
	duplicateOfFirst := devinHook("duplicate", "PreToolUse", "Read", "guard.sh")

	want := `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "type": "command"
    },
    {
      "command": "other.sh",
      "matcher": "Edit",
      "type": "command"
    }
  ]
}`

	orders := map[string][]ir.Resource{
		"duplicate first":  {duplicateOfFirst, first, second},
		"duplicate middle": {first, duplicateOfFirst, second},
		"duplicate last":   {first, second, duplicateOfFirst},
		"no duplicate":     {first, second},
	}
	for name, resources := range orders {
		t.Run(name, func(t *testing.T) {
			result, err := compileBundleFromIR(&ingest.Bundle{Resources: resources}, TargetDevin)
			if err != nil {
				t.Fatalf("compileBundleFromIR(devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %v, want one", artifactPaths(result.Artifacts))
			}
			if got := string(result.Artifacts[0].Bytes); got != want {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestDevinBundleBadHookDoesNotSuppressGoodOnes is the "skip only bad entries"
// contract. Plan, error behavior: "Within valid aggregate JSON, invalid or
// conflicting desired entries are skipped while other valid entries are patched
// atomically."
//
// The four entry-skip hook.devin.* codes are ERRORS since bead
// agent-marketplace-5exj.20, so this case runs straight through the
// per-resource error contract: each bad hook is reported as an error and named
// in Result.Skipped, the aggregate omits exactly those entries, and the skill,
// the subagent, and the one good hook are published untouched.
func TestDevinBundleBadHookDoesNotSuppressGoodOnes(t *testing.T) {
	result, err := compileBundleFromIR(&ingest.Bundle{
		Resources: []ir.Resource{
			ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, ir.SourceRef{}),
			ir.NewSubagent("demo-agent", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}),
			devinHook("good", "PreToolUse", "Read", "guard.sh"),
			devinHook("unmappable-event", "SubagentStop", "", "never.sh"),
			devinHook("matcher-on-non-tool-event", "SessionStart", "Read", "never.sh"),
			ir.NewHook("bad-type", "PreToolUse", "never.sh", "http", nil, ir.SourceRef{}),
			ir.NewHook("no-payload", "PreToolUse", "", "command", nil, ir.SourceRef{}),
		},
	}, TargetDevin)
	if err != nil {
		t.Fatalf("compileBundleFromIR(devin): %v", err)
	}
	if !diag.HasErrors(result.Diagnostics) {
		t.Fatalf("a skipped hook entry must be reported as an error so sync exits non-zero: %+v", result.Diagnostics)
	}
	assertSkippedResources(t, result.Skipped, []SkippedResource{
		{Kind: KindHook, Name: "unmappable-event", Codes: []string{diag.CodeHookDevinEventUnmappable}},
		{Kind: KindHook, Name: "matcher-on-non-tool-event", Codes: []string{diag.CodeHookDevinMatcherUnsupportedEvent}},
		{Kind: KindHook, Name: "bad-type", Codes: []string{diag.CodeHookDevinUnsupportedType}},
		{Kind: KindHook, Name: "no-payload", Codes: []string{diag.CodeHookDevinPayloadMissing}},
	})

	want := map[string]string{
		"skills/demo/SKILL.md": "---\nname: demo\ndescription: Demo skill\n---\nBody\n",
		"agents/demo-agent.md": "---\nname: demo-agent\ndescription: Demo subagent\nmodel: sonnet\n---\nBody\n",
		"hooks.v1.json": `{
  "PreToolUse": [
    {
      "command": "guard.sh",
      "matcher": "Read",
      "type": "command"
    }
  ]
}`,
	}
	if len(result.Artifacts) != len(want) {
		t.Fatalf("artifacts = %v, want %d", artifactPaths(result.Artifacts), len(want))
	}
	for _, a := range result.Artifacts {
		expected, ok := want[a.Path]
		if !ok {
			t.Errorf("unexpected artifact path %q", a.Path)
			continue
		}
		if got := string(a.Bytes); got != expected {
			t.Errorf("artifact %q =\n%s\nwant\n%s", a.Path, got, expected)
		}
	}

	wantDiags := map[string]string{
		"unmappable-event":          diag.CodeHookDevinEventUnmappable,
		"matcher-on-non-tool-event": diag.CodeHookDevinMatcherUnsupportedEvent,
		"bad-type":                  diag.CodeHookDevinUnsupportedType,
		"no-payload":                diag.CodeHookDevinPayloadMissing,
	}
	seen := make(map[string]bool, len(wantDiags))
	for _, d := range result.Diagnostics {
		code, expected := wantDiags[d.Resource]
		if !expected {
			continue
		}
		if d.Code != code {
			t.Errorf("resource %q diagnostic code = %q, want %q", d.Resource, d.Code, code)
		}
		if d.Severity != SeverityError {
			t.Errorf("resource %q diagnostic severity = %v, want %v", d.Resource, d.Severity, SeverityError)
		}
		if d.Target != string(TargetDevin) {
			t.Errorf("resource %q diagnostic target = %q, want %q", d.Resource, d.Target, TargetDevin)
		}
		seen[d.Resource] = true
	}
	for resource := range wantDiags {
		if !seen[resource] {
			t.Errorf("missing diagnostic for skipped hook %q: %+v", resource, result.Diagnostics)
		}
	}
}

func artifactPaths(artifacts []Artifact) []string {
	paths := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		paths = append(paths, a.Path)
	}
	return paths
}

// TestDevinBundleBuiltinNameConflictSuppressesArtifacts covers the built-in
// name conflict at the bundle level.
//
// It drives the IR directly rather than a source layout: Devin's built-in
// identifiers (subagent_explore, subagent_general) use underscores, and the
// Claude bundle loader requires kebab-case, so the conflict cannot be expressed
// as a Claude-layout fixture. It is reachable from the Devin ingester (D8) and
// from any consumer that builds IR itself, which is exactly why the compiler
// must refuse it rather than rely on a loader-side name rule.
func TestDevinBundleBuiltinNameConflictSuppressesArtifacts(t *testing.T) {
	for _, name := range []string{"subagent_explore", "subagent_general"} {
		t.Run(name, func(t *testing.T) {
			result, err := compileBundleFromIR(&ingest.Bundle{
				Resources: []ir.Resource{
					ir.NewSubagent(name, "Built-in shadow", "Body\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{File: "agents/" + name + ".md", Line: 1}),
					ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, ir.SourceRef{}),
				},
			}, TargetDevin)
			if err != nil {
				t.Fatalf("compileBundleFromIR(devin): %v", err)
			}
			// Only the offending subagent loses its artifact; the sibling skill
			// in the same bundle is published.
			if got, want := artifactPaths(result.Artifacts), "skills/demo/SKILL.md"; len(got) != 1 || got[0] != want {
				t.Fatalf("artifacts = %v, want exactly [%s]", got, want)
			}
			assertSkippedResources(t, result.Skipped, []SkippedResource{
				{Kind: KindSubagent, Name: name, Codes: []string{diag.CodeSubagentDevinBuiltinNameConflict}},
			})
			found := false
			for _, d := range result.Diagnostics {
				if d.Code == diag.CodeSubagentDevinBuiltinNameConflict && d.Severity == SeverityError && d.Resource == name && d.Target == string(TargetDevin) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s error diagnostic: %+v", diag.CodeSubagentDevinBuiltinNameConflict, result.Diagnostics)
			}
		})
	}
}

// TestDevinBundleMergesMcpServersIntoOneAggregate is the D7 bundle contract:
// every selected MCP server contributes one keyed entry to the single
// mcp_config.json aggregate, and the merged bytes are exact.
//
// Plan, resource mapping: "MCP server | `mcp_config.json` | Stdio or HTTP
// server merged into a shared file". Plan, entry identities: "MCP identity is
// the server object key."
func TestDevinBundleMergesMcpServersIntoOneAggregate(t *testing.T) {
	result, err := compileBundleFromIR(&ingest.Bundle{
		Resources: []ir.Resource{
			ir.NewMcpServer("zeta", "npx", []string{"-y", "zeta-mcp"}, map[string]string{"SETTING": "value"}, nil, ir.SourceRef{}),
			ir.NewMcpServerWithOptions("alpha", "", nil, nil, nil, ir.SourceRef{},
				ir.WithMcpURL("https://example.invalid/mcp"),
				ir.WithMcpTransport("http"),
				ir.WithMcpHeaders(map[string]string{"X-Client": "example"}),
				ir.WithMcpOAuth("client-id", "${env:OAUTH_CLIENT_SECRET}", "https://example.invalid"),
				ir.WithMcpDisabled(false)),
		},
	}, TargetDevin)
	if err != nil {
		t.Fatalf("compileBundleFromIR(devin): %v", err)
	}
	if diag.HasErrors(result.Diagnostics) {
		t.Fatalf("unexpected error diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %v, want exactly mcp_config.json", artifactPaths(result.Artifacts))
	}
	if result.Artifacts[0].Path != "mcp_config.json" {
		t.Fatalf("artifact path = %q, want %q", result.Artifacts[0].Path, "mcp_config.json")
	}
	want := `{
  "mcpServers": {
    "alpha": {
      "disabled": false,
      "headers": {
        "X-Client": "example"
      },
      "oauthClientId": "client-id",
      "oauthClientSecret": "${env:OAUTH_CLIENT_SECRET}",
      "oauthResource": "https://example.invalid",
      "transport": "http",
      "url": "https://example.invalid/mcp"
    },
    "zeta": {
      "args": [
        "-y",
        "zeta-mcp"
      ],
      "command": "npx",
      "env": {
        "SETTING": "value"
      }
    }
  }
}`
	if got := string(result.Artifacts[0].Bytes); got != want {
		t.Errorf("merged mcp_config.json =\n%s\nwant\n%s", got, want)
	}
}

// TestDevinBundleValidMcpServersSurviveSiblingFailures is the acceptance
// criterion "valid entries survive sibling failures", exercised through the
// bundle path.
//
// Four unrepresentable servers — one per skip branch — sit next to one valid
// server, one skill, one subagent, and one hook. All four now report ERRORS:
// three transport branches from bead agent-marketplace-5exj.20 and the
// unrepresentable-command branch from bead agent-marketplace-5exj.23, which
// gave the entry-removing interpolation case its own code
// (mcp.devin.interpolation-unrepresentable) instead of leaving it on the shared
// value-level warning. The valid MCP entry, the skill, the subagent, and the
// hook all survive: suppression is per resource. Plan, error behavior: "A
// failed MCP merge leaves mcp_config.json untouched but does not prevent
// unrelated skills, agents, or a valid hooks file from being materialized."
func TestDevinBundleValidMcpServersSurviveSiblingFailures(t *testing.T) {
	result, err := compileBundleFromIR(&ingest.Bundle{
		Resources: []ir.Resource{
			ir.NewMcpServer("good", "npx", nil, nil, nil, ir.SourceRef{}),
			ir.NewMcpServerWithOptions("ambiguous", "npx", nil, nil, nil, ir.SourceRef{},
				ir.WithMcpURL("https://example.invalid/mcp")),
			ir.NewMcpServer("incomplete", "", nil, nil, nil, ir.SourceRef{}),
			ir.NewMcpServerWithOptions("bad-transport", "", nil, nil, nil, ir.SourceRef{},
				ir.WithMcpURL("https://example.invalid/mcp"), ir.WithMcpTransport("websocket")),
			ir.NewMcpServer("bad-command", "${file:/etc/command}", nil, nil, nil, ir.SourceRef{}),
			ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, ir.SourceRef{}),
			ir.NewSubagent("demo-agent", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}),
			ir.NewHook("demo-hook", "PreToolUse", "check.sh", "command", nil, ir.SourceRef{}),
		},
	}, TargetDevin)
	if err != nil {
		t.Fatalf("compileBundleFromIR(devin): %v", err)
	}
	if !diag.HasErrors(result.Diagnostics) {
		t.Fatalf("a skipped MCP entry must be reported as an error so sync exits non-zero: %+v", result.Diagnostics)
	}
	assertSkippedResources(t, result.Skipped, []SkippedResource{
		{Kind: KindMcpServer, Name: "ambiguous", Codes: []string{diag.CodeMcpDevinTransportAmbiguous}},
		{Kind: KindMcpServer, Name: "incomplete", Codes: []string{diag.CodeMcpDevinTransportIncomplete}},
		{Kind: KindMcpServer, Name: "bad-transport", Codes: []string{diag.CodeMcpDevinTransportUnsupported}},
		{Kind: KindMcpServer, Name: "bad-command", Codes: []string{diag.CodeMcpDevinInterpolationUnrepresentable}},
	})

	wantPaths := map[string]string{
		"mcp_config.json": `{
  "mcpServers": {
    "good": {
      "command": "npx"
    }
  }
}`,
		"skills/demo/SKILL.md": "",
		"agents/demo-agent.md": "",
		// Fragment merging re-marshals with indentation, so the aggregate
		// bytes differ from the per-resource fragment bytes pinned in
		// compile/devin/hook_test.go.
		"hooks.v1.json": `{
  "PreToolUse": [
    {
      "command": "check.sh",
      "type": "command"
    }
  ]
}`,
	}
	if len(result.Artifacts) != len(wantPaths) {
		t.Errorf("artifacts = %v, want exactly %d", artifactPaths(result.Artifacts), len(wantPaths))
	}
	got := map[string]string{}
	for _, a := range result.Artifacts {
		got[a.Path] = string(a.Bytes)
	}
	for path, wantBytes := range wantPaths {
		body, ok := got[path]
		if !ok {
			t.Errorf("missing artifact %q; got %v", path, artifactPaths(result.Artifacts))
			continue
		}
		if wantBytes != "" && body != wantBytes {
			t.Errorf("artifact %q =\n%s\nwant\n%s", path, body, wantBytes)
		}
	}
	if len(got) != len(wantPaths) {
		t.Errorf("artifacts = %v, want exactly %d", artifactPaths(result.Artifacts), len(wantPaths))
	}

	wantSkips := map[string]struct {
		code     string
		severity Severity
	}{
		"ambiguous":     {diag.CodeMcpDevinTransportAmbiguous, SeverityError},
		"incomplete":    {diag.CodeMcpDevinTransportIncomplete, SeverityError},
		"bad-transport": {diag.CodeMcpDevinTransportUnsupported, SeverityError},
		// The entry-removing interpolation case has its own code since bead
		// agent-marketplace-5exj.23; the shared mcp.interpolation-unsupported
		// warning now only ever means a single value was omitted from an entry
		// that was still written.
		"bad-command": {diag.CodeMcpDevinInterpolationUnrepresentable, SeverityError},
	}
	seen := map[string]bool{}
	for _, d := range result.Diagnostics {
		want, tracked := wantSkips[d.Resource]
		if !tracked {
			continue
		}
		if d.Code != want.code {
			t.Errorf("resource %q diagnostic code = %q, want %q", d.Resource, d.Code, want.code)
		}
		if d.Severity != want.severity {
			t.Errorf("resource %q diagnostic severity = %v, want %v", d.Resource, d.Severity, want.severity)
		}
		if d.Target != string(TargetDevin) {
			t.Errorf("resource %q diagnostic target = %q, want %q", d.Resource, d.Target, TargetDevin)
		}
		seen[d.Resource] = true
	}
	for resource := range wantSkips {
		if !seen[resource] {
			t.Errorf("missing diagnostic for skipped MCP server %q: %+v", resource, result.Diagnostics)
		}
	}
}

// TestMcpInterpolationUnsupportedIsDevinOnly pins the fact the bead
// agent-marketplace-5exj.23 severity split rests on: the shared
// mcp.interpolation-unsupported code is emitted by the Devin compiler and by
// nothing else, so leaving it a warning is a decision about Devin's VALUE-level
// losses, not an accommodation of five other targets.
//
// The other five have no unmappable interpolation outcome to report at all:
// Claude, Factory, and Cursor write MCP values through verbatim, Gemini
// rewrites env tokens through its own table, and the Windsurf column of
// compile/mapping/mcp.go declares a pattern for all three source styles, so
// McpInterpolationSupported never returns ok=false for it.
//
// It lives in this package rather than in compile/devin because only here are
// all six compilers registered.
func TestMcpInterpolationUnsupportedIsDevinOnly(t *testing.T) {
	// The same server for every target: an unmappable ${file:...} style in an
	// argument and in an env value, which is the shared code's value-level
	// case on Devin.
	server := ir.NewMcpServer("demo", "npx", []string{"${file:/etc/arg}"},
		map[string]string{"TOKEN": "${file:/etc/secret}"}, nil, ir.SourceRef{})

	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetWindsurf, TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Compile(server, target)
			if err != nil {
				t.Fatalf("Compile(mcp, %s): %v", target, err)
			}
			for _, d := range result.Diagnostics {
				if d.Code == diag.CodeMcpInterpolationUnsupported {
					t.Errorf("%s emits the shared interpolation code, so its severity is no longer a Devin-only decision: %+v", target, d)
				}
				if d.Code == diag.CodeMcpDevinInterpolationUnrepresentable {
					t.Errorf("%s emits a Devin-specific code: %+v", target, d)
				}
			}
		})
	}

	t.Run("devin", func(t *testing.T) {
		result, err := Compile(server, TargetDevin)
		if err != nil {
			t.Fatalf("Compile(mcp, Devin): %v", err)
		}
		var emitted bool
		for _, d := range result.Diagnostics {
			if d.Code != diag.CodeMcpInterpolationUnsupported {
				continue
			}
			emitted = true
			if d.Severity != SeverityWarning {
				t.Errorf("shared code severity = %v, want warning: the value-level outcome keeps the entry", d.Severity)
			}
		}
		if !emitted {
			// Without this the five negative cases above prove nothing.
			t.Fatalf("Devin no longer emits the shared code, so this test is vacuous: %+v", result.Diagnostics)
		}
		if len(result.Artifacts) != 1 {
			t.Errorf("artifacts = %d, want 1: a value-level loss keeps the entry", len(result.Artifacts))
		}
	})
}
