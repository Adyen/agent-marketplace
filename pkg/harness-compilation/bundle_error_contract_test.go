package harnesscompile

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// The bundle error contract under test, from the promoted plan
// (docs/plans/2026-08-03-devin-first-class-harness.md, "Error behavior"):
// "Resources with error diagnostics are skipped, valid resources continue, and
// sync exits non-zero if any error occurred."
//
// These cases pin all four halves of it end to end:
//
//  1. the failing resource's artifacts are gone — every one of them, fragments
//     of a shared aggregate included;
//  2. the valid resources' artifacts are published with EXACT bytes;
//  3. the error diagnostic is still reported and attributed;
//  4. the caller can tell the run was not clean — Result.HasErrors,
//     Result.Skipped, and an ErrPartialResult from Materialize.

type bundleErrorCase struct {
	name      string
	target    Target
	options   []Option
	resources []ir.Resource

	// wantArtifacts is the COMPLETE expected output: exact paths, exact bytes.
	wantArtifacts map[string]string
	// wantAbsent lists paths the failing resources would have produced.
	wantAbsent []string
	// wantErrors maps resource name to the error code it must report.
	wantErrors map[string]string
	// wantWarnings maps resource name to a warning code it must report while
	// NOT being skipped.
	wantWarnings map[string]string
	wantSkipped  []SkippedResource
}

func bundleErrorCases() []bundleErrorCase {
	isolatedAgent := ir.NewSubagent("isolated", "Isolated reviewer", "Body\n", "sonnet", nil, "", nil, "worktree", nil, ir.SourceRef{File: "agents/isolated.md", Line: 1})
	// A subagent whose name shadows a Devin built-in is the second independent
	// Devin resource error.
	builtinAgent := ir.NewSubagent("subagent_explore", "Shadows a built-in", "Body\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{File: "agents/subagent_explore.md", Line: 1})
	rule := ir.NewRule("style", "House style", "Use tabs\n", nil, true, nil, ir.SourceRef{File: "rules/style.md", Line: 1})
	validSkill := ir.NewSkill("deploy", "Deploy the service", "Deploy body\n", nil, nil, ir.SourceRef{})
	validSkillBytes := "---\nname: deploy\ndescription: Deploy the service\n---\nDeploy body\n"

	return []bundleErrorCase{
		{
			// The headline case: one erroring Devin resource costs itself and
			// nothing else. A native rule rides along to prove it is not
			// implicated and does not contribute to the failure.
			name:      "an erroring devin subagent is skipped while a valid devin skill is published",
			target:    TargetDevin,
			resources: []ir.Resource{isolatedAgent, rule, validSkill},
			wantArtifacts: map[string]string{
				"skills/deploy/SKILL.md": validSkillBytes,
				"rules/style.md":         "---\ntrigger: always_on\ndescription: House style\n---\nUse tabs\n",
			},
			wantAbsent: []string{"agents/isolated.md"},
			wantErrors: map[string]string{"isolated": diag.CodeSubagentSecurityRestrictionUnsupported},
			wantSkipped: []SkippedResource{
				{Kind: KindSubagent, Name: "isolated", Codes: []string{diag.CodeSubagentSecurityRestrictionUnsupported}},
			},
		},
		{
			// Two independent failures must not compound into a third: the
			// valid skill still survives both.
			name:      "two erroring resources are both skipped and the valid one survives",
			target:    TargetDevin,
			resources: []ir.Resource{builtinAgent, isolatedAgent, validSkill},
			wantArtifacts: map[string]string{
				"skills/deploy/SKILL.md": validSkillBytes,
			},
			wantAbsent: []string{"agents/isolated.md", "agents/subagent_explore.md"},
			wantErrors: map[string]string{
				"subagent_explore": diag.CodeSubagentDevinBuiltinNameConflict,
				"isolated":         diag.CodeSubagentSecurityRestrictionUnsupported,
			},
			wantSkipped: []SkippedResource{
				{Kind: KindSubagent, Name: "subagent_explore", Codes: []string{diag.CodeSubagentDevinBuiltinNameConflict}},
				{Kind: KindSubagent, Name: "isolated", Codes: []string{diag.CodeSubagentSecurityRestrictionUnsupported}},
			},
		},
		{
			// An erroring resource next to an aggregate it does NOT contribute
			// to: the aggregate must merge every surviving contributor and
			// nothing must be lost by association.
			name:   "an erroring sibling does not disturb a shared devin aggregate",
			target: TargetDevin,
			resources: []ir.Resource{
				isolatedAgent,
				devinHook("pre-read", "PreToolUse", "Read", "read-guard.sh"),
				devinHook("pre-edit", "PreToolUse", "Edit", "edit-guard.sh"),
			},
			wantArtifacts: map[string]string{
				"hooks.v1.json": `{
  "PreToolUse": [
    {
      "hooks": [
        {
          "command": "read-guard.sh",
          "type": "command"
        }
      ],
      "matcher": "Read"
    },
    {
      "hooks": [
        {
          "command": "edit-guard.sh",
          "type": "command"
        }
      ],
      "matcher": "Edit"
    }
  ]
}`,
			},
			wantAbsent: []string{"agents/isolated.md"},
			wantErrors: map[string]string{"isolated": diag.CodeSubagentSecurityRestrictionUnsupported},
			wantSkipped: []SkippedResource{
				{Kind: KindSubagent, Name: "isolated", Codes: []string{diag.CodeSubagentSecurityRestrictionUnsupported}},
			},
		},
		{
			// A bundle-level conflict rather than a single resource. Both named
			// producers lose everything they emitted, including their fragment
			// of the shared .claude-plugin/plugin.json manifest, so the manifest
			// cannot advertise an agent whose file was never written. The third
			// subagent's fragment still merges.
			name:   "a bundle-level path conflict drops both producers and their aggregate fragments",
			target: TargetClaude,
			resources: []ir.Resource{
				ir.NewSubagent("reviewer", "From registry A", "Body A\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{File: "agents/reviewer.md", Line: 1}),
				ir.NewSubagent("reviewer", "From registry B", "Body B\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{File: "other/agents/reviewer.md", Line: 1}),
				ir.NewSubagent("keeper", "Unrelated agent", "Body K\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{File: "agents/keeper.md", Line: 1}),
			},
			wantArtifacts: map[string]string{
				".claude-plugin/plugin.json": `{
  "agents": [
    {
      "description": "Unrelated agent",
      "model": "sonnet",
      "name": "keeper"
    }
  ]
}`,
				"agents/keeper.md": "---\nname: keeper\ndescription: Unrelated agent\nmodel: sonnet\n---\nBody K\n",
			},
			wantAbsent: []string{"agents/reviewer.md"},
			wantErrors: map[string]string{"reviewer": diag.CodeBundleConflictDuplicateArtifactPath},
			wantSkipped: []SkippedResource{
				{Kind: KindSubagent, Name: "reviewer", Codes: []string{diag.CodeBundleConflictDuplicateArtifactPath}},
				{Kind: KindSubagent, Name: "reviewer", Codes: []string{diag.CodeBundleConflictDuplicateArtifactPath}},
			},
		},
	}
}

func TestBundleErrorContract_SkipsOnlyFailingResources(t *testing.T) {
	for _, tt := range bundleErrorCases() {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compileBundleFromIR(&ingest.Bundle{Resources: tt.resources}, tt.target, tt.options...)
			if err != nil {
				t.Fatalf("compileBundleFromIR(%s): %v", tt.target, err)
			}

			// (4) The caller can tell the run was not clean.
			if !result.HasErrors() {
				t.Fatalf("HasErrors() = false, want true; diagnostics = %+v", result.Diagnostics)
			}
			assertSkippedResources(t, result.Skipped, tt.wantSkipped)

			// (1) and (2): the surviving output is exactly the valid resources'.
			got := make(map[string]string, len(result.Artifacts))
			for _, a := range result.Artifacts {
				if _, duplicate := got[a.Path]; duplicate {
					t.Errorf("artifact %q was published twice", a.Path)
				}
				got[a.Path] = string(a.Bytes)
			}
			for path, wantBytes := range tt.wantArtifacts {
				gotBytes, ok := got[path]
				if !ok {
					t.Errorf("missing artifact %q; got %v", path, sortedKeys(got))
					continue
				}
				if gotBytes != wantBytes {
					t.Errorf("artifact %q =\n%s\nwant\n%s", path, gotBytes, wantBytes)
				}
			}
			if len(got) != len(tt.wantArtifacts) {
				t.Errorf("artifacts = %v, want exactly %v", sortedKeys(got), sortedKeys(tt.wantArtifacts))
			}
			for _, path := range tt.wantAbsent {
				if _, present := got[path]; present {
					t.Errorf("artifact %q of a skipped resource was published", path)
				}
			}

			// (3) The error diagnostics are still reported and attributed.
			for resource, code := range tt.wantErrors {
				found := false
				for _, d := range result.Diagnostics {
					if d.Resource != resource || d.Code != code {
						continue
					}
					if d.Severity != SeverityError {
						t.Errorf("diagnostic %s for %q severity = %v, want %v", code, resource, d.Severity, SeverityError)
					}
					if d.Target != string(tt.target) {
						t.Errorf("diagnostic %s for %q target = %q, want %q", code, resource, d.Target, tt.target)
					}
					found = true
				}
				if !found {
					t.Errorf("missing %s error diagnostic for %q: %+v", code, resource, result.Diagnostics)
				}
			}

			// A warning-only resource is lossy but clean: it is reported, it is
			// NOT in Skipped, and it does not make the run fail on its own.
			for resource, code := range tt.wantWarnings {
				found := false
				for _, d := range result.Diagnostics {
					if d.Resource != resource || d.Code != code {
						continue
					}
					if d.Severity != SeverityWarning {
						t.Errorf("diagnostic %s for %q severity = %v, want %v", code, resource, d.Severity, SeverityWarning)
					}
					found = true
				}
				if !found {
					t.Errorf("missing %s warning diagnostic for %q: %+v", code, resource, result.Diagnostics)
				}
				for _, s := range result.Skipped {
					if s.Name == resource {
						t.Errorf("resource %q emitted only warnings but was reported as skipped: %+v", resource, s)
					}
				}
			}
		})
	}
}

func TestRuleLossIsCleanCompile(t *testing.T) {
	tests := []struct {
		name      string
		target    Target
		wantCode  string
		emitsRule bool
	}{
		{"devin", TargetDevin, "", true},
		{"claude", TargetClaude, diag.CodeRuleClaudeNoPrimitive, false},
		{"gemini", TargetGemini, diag.CodeRuleGeminiNoPrimitive, false},
	}

	rule := ir.NewRule("style", "House style", "Use tabs\n", nil, true, nil, ir.SourceRef{File: "rules/style.md", Line: 1})
	validSkill := ir.NewSkill("deploy", "Deploy the service", "Deploy body\n", nil, nil, ir.SourceRef{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compileBundleFromIR(&ingest.Bundle{Resources: []ir.Resource{rule, validSkill}}, tt.target)
			if err != nil {
				t.Fatalf("compileBundleFromIR(%s): %v", tt.target, err)
			}
			if result.HasErrors() {
				t.Errorf("HasErrors() = true, want false: rule handling is not a failure; diagnostics = %+v", result.Diagnostics)
			}
			if len(result.Skipped) != 0 {
				t.Errorf("Skipped = %+v, want none", result.Skipped)
			}

			var warned bool
			for _, d := range result.Diagnostics {
				if d.Code == tt.wantCode && d.Resource == "style" && d.Severity == SeverityWarning {
					warned = true
				}
			}
			if tt.wantCode != "" && !warned {
				t.Errorf("missing %s warning for the dropped rule: %+v", tt.wantCode, result.Diagnostics)
			}
			if tt.wantCode == "" && warned {
				t.Errorf("unexpected rule warning: %+v", result.Diagnostics)
			}

			// Devin preserves the body while reporting metadata loss. Targets
			// without a rule primitive publish no rule artifact.
			var skillPublished bool
			var rulePublished bool
			for _, a := range result.Artifacts {
				if strings.HasPrefix(a.Path, "rules/") || strings.Contains(a.Path, "/rules/") {
					rulePublished = true
				}
				if strings.Contains(a.Path, "deploy") {
					skillPublished = true
				}
			}
			if !skillPublished {
				t.Errorf("the valid sibling skill was not published by %s", tt.target)
			}
			if rulePublished != tt.emitsRule {
				t.Errorf("rule published = %v, want %v for %s", rulePublished, tt.emitsRule, tt.target)
			}

			// Materialize must agree: no partial result, nothing held back.
			outDir := t.TempDir()
			report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true))
			if err != nil {
				t.Fatalf("Materialize(%s): %v", tt.target, err)
			}
			if report.Partial {
				t.Error("report.Partial = true, want false")
			}
			if report.PruneSuppressed {
				t.Error("report.PruneSuppressed = true, want false: nothing was skipped")
			}
		})
	}
}

// TestBundleErrorContract_MaterializesPartialResults is the disk half of the
// same table: the valid resources' bytes land, the failing resources' paths do
// not, pruning is held back, and the caller is told the run was partial.
func TestBundleErrorContract_MaterializesPartialResults(t *testing.T) {
	for _, tt := range bundleErrorCases() {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compileBundleFromIR(&ingest.Bundle{Resources: tt.resources}, tt.target, tt.options...)
			if err != nil {
				t.Fatalf("compileBundleFromIR(%s): %v", tt.target, err)
			}

			outDir := t.TempDir()
			report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true))
			if !errors.Is(err, ErrPartialResult) {
				t.Fatalf("Materialize error = %v, want ErrPartialResult", err)
			}
			if !report.Partial {
				t.Error("report.Partial = false, want true")
			}
			if !report.PruneSuppressed {
				t.Error("report.PruneSuppressed = false, want true for a partial result")
			}
			if len(report.SkippedResources) != len(tt.wantSkipped) {
				t.Errorf("report.SkippedResources = %+v, want %d entries", report.SkippedResources, len(tt.wantSkipped))
			}

			for path, wantBytes := range tt.wantArtifacts {
				onDisk, readErr := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(path)))
				if readErr != nil {
					t.Errorf("read %q: %v", path, readErr)
					continue
				}
				if !bytes.Equal(onDisk, []byte(wantBytes)) {
					t.Errorf("%q on disk =\n%s\nwant\n%s", path, onDisk, wantBytes)
				}
			}
			for _, path := range tt.wantAbsent {
				if _, statErr := os.Stat(filepath.Join(outDir, filepath.FromSlash(path))); !os.IsNotExist(statErr) {
					t.Errorf("%q stat = %v, want the skipped resource's path to be absent", path, statErr)
				}
			}
		})
	}
}

// TestBundleErrorContract_LogsSkipDecisionsWithoutSensitiveValues asserts the
// observability requirement and its privacy half at once: every skip is logged
// with identity, codes, and counts, and no log attribute carries a resource
// body, a hook payload, an MCP environment value, a header, an OAuth secret, or
// a permission value.
func TestBundleErrorContract_LogsSkipDecisionsWithoutSensitiveValues(t *testing.T) {
	secrets := []string{
		"super-secret-body",
		"rm -rf /payload",
		"SECRET_ENV_VALUE",
		"Bearer header-token",
		"oauth-client-secret",
		"deny-permission-value",
	}

	resources := []ir.Resource{
		// A native rule whose body must never be logged. It proves the privacy
		// rule on a successful compile path, not just on the skip path.
		ir.NewRule("style", "House style", "super-secret-body\n", nil, true, nil, ir.SourceRef{}),
		// A subagent that fails on a security restriction, carrying a denied
		// tool and a permission-shaped value.
		ir.NewSubagent("isolated", "Isolated", "super-secret-body\n", "sonnet",
			[]string{"Read"}, "", []string{"deny-permission-value"}, "worktree", nil, ir.SourceRef{}),
		// An MCP server with an env value, and a hook with a payload: both
		// compile cleanly, so their values must not appear either.
		ir.NewMcpServer("srv", "npx", []string{"-y", "srv"}, map[string]string{"TOKEN": "SECRET_ENV_VALUE"}, nil, ir.SourceRef{}),
		devinHook("guard", "PreToolUse", "Read", "rm -rf /payload"),
	}

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	result, err := compileBundleFromIR(&ingest.Bundle{Resources: resources}, TargetDevin)
	if err != nil {
		t.Fatalf("compileBundleFromIR(devin): %v", err)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Name != "isolated" {
		t.Fatalf("skipped = %+v, want only the isolated subagent (the rule is native output, not a skip)", result.Skipped)
	}

	logs := buf.String()
	if !strings.Contains(logs, "bundle resource skipped") {
		t.Fatalf("skip decisions were not logged:\n%s", logs)
	}
	if !strings.Contains(logs, "resource=isolated") {
		t.Errorf("log does not attribute the skip of %q:\n%s", "isolated", logs)
	}
	if !strings.Contains(logs, diag.CodeSubagentSecurityRestrictionUnsupported) {
		t.Errorf("log does not carry code %q:\n%s", diag.CodeSubagentSecurityRestrictionUnsupported, logs)
	}
	// The rule is logged as a successful compile decision and still by name only.
	if !strings.Contains(logs, "resource=style") {
		t.Errorf("log does not record the compiled rule:\n%s", logs)
	}
	for _, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Errorf("log leaked sensitive value %q:\n%s", secret, logs)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
