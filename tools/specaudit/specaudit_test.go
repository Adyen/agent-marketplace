package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ann builds an annotation line without writing one literally into this file,
// so the repository-wide trace does not treat fixtures as real annotations.
func ann(id string) string { return "// " + "spec: " + id + "\n" }

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func kinds(findings []finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.kind
	}
	slices.Sort(out)
	return out
}

func details(findings []finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(f.kind + ": " + f.detail + "\n")
	}
	return b.String()
}

// spec: spec-governance/slug-derived-from-title
func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Preserves handwritten hooks (Devin)": "preserves-handwritten-hooks-devin",
		"Slug derived from title":             "slug-derived-from-title",
		"  --Leading and trailing!!  ":        "leading-and-trailing",
		"v2/v3 config -> v4":                  "v2-v3-config-v4",
		"UPPER_case 42":                       "upper-case-42",
	}
	for title, want := range cases {
		if got := slugify(title); got != want {
			t.Errorf("slugify(%q) = %q, want %q", title, got, want)
		}
	}
}

const mainSpec = `# alpha Specification

## Purpose

Example.

## Requirements

### Requirement: First

#### Scenario: One

- **WHEN** x
- **THEN** y

### Requirement: Second

` + "```" + `
#### Scenario: Inside fence
` + "```" + `

#### Scenario: Two

- **WHEN** x
- **THEN** y
`

// spec: spec-governance/duplicate-slug-rejected
func TestTraceDuplicateSlug(t *testing.T) {
	root := writeTree(t, map[string]string{
		"openspec/specs/alpha/spec.md": "## Requirements\n\n### Requirement: R\n\n#### Scenario: Foo bar\n\n### Requirement: S\n\n#### Scenario: Foo, bar!\n",
		"a_test.go":                    ann("alpha/foo-bar"),
	})
	findings, _, err := trace(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(findings); !slices.Equal(got, []string{kindDuplicateSlug}) {
		t.Fatalf("kinds = %v, want [%s]\n%s", got, kindDuplicateSlug, details(findings))
	}
	for _, want := range []string{"alpha", `"Foo bar"`, `"Foo, bar!"`} {
		if !strings.Contains(findings[0].detail, want) {
			t.Errorf("detail %q does not name %s", findings[0].detail, want)
		}
	}
}

func TestTrace(t *testing.T) {
	base := map[string]string{"openspec/specs/alpha/spec.md": mainSpec}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	cases := []struct {
		name       string
		files      map[string]string
		wantKinds  []string
		wantDetail []string
	}{
		// spec: spec-governance/fully-bound-specs-pass
		{
			name: "fully bound in Go and TypeScript, fenced heading ignored",
			files: with(map[string]string{
				"internal/x/x_test.go":         "package x\n\n" + ann("alpha/one"),
				"tools/tui-qa/tests/a.spec.ts": "  " + ann("alpha/two"),
			}),
		},
		// spec: spec-governance/unbound-scenario-fails
		{
			name:       "unbound scenario",
			files:      with(map[string]string{"x_test.go": ann("alpha/one")}),
			wantKinds:  []string{kindUnboundScenario},
			wantDetail: []string{"alpha/two", "openspec/specs/alpha/spec.md:"},
		},
		// spec: spec-governance/dangling-annotation-fails
		{
			name:       "dangling annotation",
			files:      with(map[string]string{"x_test.go": ann("alpha/one") + ann("alpha/two") + ann("alpha/gone")}),
			wantKinds:  []string{kindDanglingAnnotation},
			wantDetail: []string{"x_test.go:3: alpha/gone"},
		},
		{
			name: "annotations outside test files, testdata, and node_modules do not count",
			files: with(map[string]string{
				"x_test.go":                    ann("alpha/one"),
				"x.go":                         ann("alpha/two"),
				"internal/testdata/y_test.go":  ann("alpha/two"),
				"web/node_modules/z/z.test.ts": ann("alpha/two"),
			}),
			wantKinds: []string{kindUnboundScenario},
		},
		// spec: spec-governance/scenario-added-in-an-active-change
		{
			name: "scenario added in active change",
			files: with(map[string]string{
				"openspec/changes/c1/specs/beta/spec.md": "## Purpose\n\nNew.\n\n## ADDED Requirements\n\n### Requirement: New\n\n#### Scenario: Fresh\n",
				"x_test.go":                              ann("alpha/one") + ann("alpha/two") + ann("beta/fresh"),
			}),
		},
		// spec: spec-governance/scenario-removed-in-an-active-change
		{
			name: "removed requirement is neither required nor annotatable",
			files: with(map[string]string{
				"openspec/changes/c1/specs/alpha/spec.md": "## REMOVED Requirements\n\n### Requirement:  Second\n\n**Reason**: gone\n",
				"x_test.go": ann("alpha/one") + ann("alpha/two"),
			}),
			wantKinds:  []string{kindDanglingAnnotation},
			wantDetail: []string{"alpha/two"},
		},
		{
			name: "removed requirement needs no annotation",
			files: with(map[string]string{
				"openspec/changes/c1/specs/alpha/spec.md": "## REMOVED Requirements\n\n### Requirement: Second\n",
				"x_test.go": ann("alpha/one"),
			}),
		},
		{
			name: "modified requirement replaces its scenarios",
			files: with(map[string]string{
				"openspec/changes/c1/specs/alpha/spec.md": "## MODIFIED Requirements\n\n### Requirement: Second\n\n#### Scenario: Replacement\n",
				"x_test.go": ann("alpha/one") + ann("alpha/replacement"),
			}),
		},
		{
			name: "archived changes are ignored",
			files: with(map[string]string{
				"openspec/changes/archive/2026-01-01-old/specs/beta/spec.md": "## ADDED Requirements\n\n### Requirement: Old\n\n#### Scenario: Stale\n",
				"x_test.go": ann("alpha/one") + ann("alpha/two"),
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, _, err := trace(writeTree(t, tc.files))
			if err != nil {
				t.Fatal(err)
			}
			if got := kinds(findings); !slices.Equal(got, tc.wantKinds) {
				t.Fatalf("kinds = %v, want %v\n%s", got, tc.wantKinds, details(findings))
			}
			for _, want := range tc.wantDetail {
				if !strings.Contains(details(findings), want) {
					t.Errorf("findings do not mention %q:\n%s", want, details(findings))
				}
			}
		})
	}
}

func TestArchived(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		wantKinds []string
	}{
		// spec: spec-governance/completed-unarchived-change-fails
		{
			name: "completed change",
			files: map[string]string{
				"openspec/changes/done/tasks.md": "## 1. A\n\n- [x] 1.1 a\n- [X] 1.2 b\n",
				"openspec/changes/wip/tasks.md":  "- [x] 1.1 a\n- [ ] 1.2 b\n",
			},
			wantKinds: []string{kindUnarchivedChange},
		},
		// spec: spec-governance/in-flight-change-passes
		{
			name: "in-flight, task-less, and archived changes",
			files: map[string]string{
				"openspec/changes/wip/tasks.md":                "- [x] 1.1 a\n  - [ ] 1.2 nested\n",
				"openspec/changes/empty/tasks.md":              "## 1. Nothing yet\n",
				"openspec/changes/noplan/proposal.md":          "x",
				"openspec/changes/archive/2026-01-01/tasks.md": "- [x] 1.1 a\n",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, _, err := archived(writeTree(t, tc.files))
			if err != nil {
				t.Fatal(err)
			}
			if got := kinds(findings); !slices.Equal(got, tc.wantKinds) {
				t.Fatalf("kinds = %v, want %v\n%s", got, tc.wantKinds, details(findings))
			}
			if len(findings) > 0 && !strings.Contains(findings[0].detail, "done") {
				t.Errorf("finding does not name the change: %s", findings[0].detail)
			}
		})
	}
}

func TestEvaluateGate(t *testing.T) {
	cases := []struct {
		name        string
		paths       []string
		trailers    []string
		wantKinds   []string
		wantSummary string
		wantDetail  string
	}{
		// spec: spec-governance/behavior-change-without-delta-fails
		{
			name:       "behavior without delta",
			paths:      []string{"internal/config/loader.go", "api/openapi.yaml", "docs/x.md"},
			wantKinds:  []string{kindMissingSpecDelta},
			wantDetail: "internal/config/loader.go, api/openapi.yaml",
		},
		// spec: spec-governance/behavior-change-with-delta-passes
		{
			name:  "behavior with main spec",
			paths: []string{"cmd/agent-marketplace/sync.go", "openspec/specs/alpha/spec.md"},
		},
		{
			name:  "behavior with change delta",
			paths: []string{"web/app/pages/index.vue", "openspec/changes/c1/specs/alpha/spec.md"},
		},
		{
			name:  "behavior with archived delta",
			paths: []string{"pkg/marketplace/api/types.go", "openspec/changes/archive/2026-01-01-c1/specs/alpha/spec.md"},
		},
		{
			name:      "proposal alone is not a delta",
			paths:     []string{"pkg/marketplace/api/types.go", "openspec/changes/c1/proposal.md"},
			wantKinds: []string{kindMissingSpecDelta},
		},
		// spec: spec-governance/test-only-change-passes
		{
			name: "tests, markdown, testdata only",
			paths: []string{
				"internal/config/loader_test.go", "pkg/harness-compilation/README.md",
				"pkg/harness-compilation/testdata/x/SKILL.json", "web/tests/unit/a.test.ts",
				"tools/tui-qa/tests/a.spec.ts", "e2e/foo_test.go", "tools/ci-plan/main.go",
			},
		},
		// spec: spec-governance/trailer-with-reason-passes
		{
			name:        "trailer with em dash reason",
			paths:       []string{"internal/x/x.go"},
			trailers:    []string{"", "Spec-Impact-irrelevant", "none — dependency bump only"},
			wantSummary: "dependency bump only",
		},
		{
			name:        "trailer with ASCII dash reason",
			paths:       []string{"internal/x/x.go"},
			trailers:    []string{"none - refactor"},
			wantSummary: "refactor",
		},
		// spec: spec-governance/trailer-without-reason-fails
		{
			name:      "trailer without reason",
			paths:     []string{"internal/x/x.go"},
			trailers:  []string{"none", "", "none —  "},
			wantKinds: []string{kindSpecImpactNoReason},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, summary := evaluateGate(tc.paths, tc.trailers)
			if got := kinds(findings); !slices.Equal(got, tc.wantKinds) {
				t.Fatalf("kinds = %v, want %v\n%s", got, tc.wantKinds, details(findings))
			}
			if !strings.Contains(summary, tc.wantSummary) {
				t.Errorf("summary %q does not contain %q", summary, tc.wantSummary)
			}
			if tc.wantDetail != "" && !strings.Contains(details(findings), tc.wantDetail) {
				t.Errorf("findings do not mention %q:\n%s", tc.wantDetail, details(findings))
			}
		})
	}
}

func TestGateReadsGitRange(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@example.com"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@example.com"}} {
		t.Setenv(kv[0], kv[1])
	}
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(rel, msg string) string {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", rel)
		run("commit", "-q", "-m", msg)
		return run("rev-parse", "HEAD")
	}
	run("init", "-q")
	base := commit("README.md", "init")
	plain := commit("internal/x/x.go", "change behavior")

	findings, _, err := gate(root, base, plain)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(findings); !slices.Equal(got, []string{kindMissingSpecDelta}) {
		t.Fatalf("kinds = %v, want [%s]", got, kindMissingSpecDelta)
	}

	trailed := commit("internal/x/y.go", "refactor\n\nSpec-Impact: none — pure rename")
	findings, summary, err := gate(root, base, trailed)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || !strings.Contains(summary, "pure rename") {
		t.Fatalf("findings = %v, summary = %q; want pass citing the trailer reason", findings, summary)
	}
}

func TestRun(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run([]string{"--help"}, &out, &errOut); err != nil || !strings.Contains(out.String(), "usage: specaudit") {
		t.Fatalf("--help: err=%v out=%q", err, out.String())
	}
	for _, args := range [][]string{{}, {"bogus"}, {"gate"}, {"trace", "extra"}} {
		if err := run(args, &out, &errOut); err == nil {
			t.Errorf("run(%v) succeeded, want error", args)
		}
	}
	root := writeTree(t, map[string]string{"openspec/specs/alpha/spec.md": mainSpec})
	errOut.Reset()
	if err := run([]string{"trace", "-root", root}, &out, &errOut); !errors.Is(err, errFindings) {
		t.Fatalf("trace with unbound scenarios: err = %v, want errFindings", err)
	}
	if !strings.Contains(errOut.String(), "specaudit trace: unbound-scenario: alpha/one") {
		t.Errorf("stderr missing stable finding line:\n%s", errOut.String())
	}
}
