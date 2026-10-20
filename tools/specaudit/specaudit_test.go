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
		// spec: spec-governance/renamed-requirement-is-modified
		{
			name: "renamed requirement is modified before rename section",
			files: with(map[string]string{
				"openspec/changes/c1/specs/alpha/spec.md": "## MODIFIED Requirements\n\n### Requirement: Renamed\n\n#### Scenario: Two\n\n## RENAMED Requirements\n- FROM: `### Requirement: Second`\n- TO: `### Requirement: Renamed`\n",
				"x_test.go": ann("alpha/one") + ann("alpha/two"),
			}),
		},
		// spec: spec-governance/renamed-requirement-is-removed
		{
			name: "renamed requirement can be removed by its new name",
			files: with(map[string]string{
				"openspec/changes/c1/specs/alpha/spec.md": "## RENAMED Requirements\n- FROM: `### Requirement: Second`\n- TO: `### Requirement: Renamed`\n\n## REMOVED Requirements\n\n### Requirement: Renamed\n",
				"x_test.go": ann("alpha/one"),
			}),
		},
		// spec: spec-governance/rename-preserves-scenario-identities
		{
			name: "rename alone preserves scenario annotations",
			files: with(map[string]string{
				"openspec/changes/c1/specs/alpha/spec.md": "## RENAMED Requirements\n- FROM: `### Requirement: Second`\n- TO: `### Requirement: Renamed`\n",
				"x_test.go": ann("alpha/one") + ann("alpha/two"),
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
			findings, summary := evaluateGate(changed(tc.paths...), map[string][]string{"Spec-Impact": tc.trailers})
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

func changed(paths ...string) []changedFile {
	out := make([]changedFile, len(paths))
	for i, p := range paths {
		out[i] = changedFile{path: p}
	}
	return out
}

func TestEvaluateTUIQAGate(t *testing.T) {
	const delta = "openspec/changes/c1/specs/alpha/spec.md"
	cases := []struct {
		name        string
		files       []changedFile
		trailers    map[string][]string
		wantKinds   []string
		wantSummary string
		wantDetail  string
		notDetail   []string
	}{
		// spec: spec-governance/tui-change-without-journey-fails
		{
			name:       "TUI change without journey",
			files:      changed("internal/tui/app.go", "internal/tui/footer.go", "docs/x.md", delta),
			wantKinds:  []string{kindMissingTUIQAJourney},
			wantDetail: "internal/tui/app.go, internal/tui/footer.go",
		},
		// spec: spec-governance/tui-launch-wiring-is-tui-bearing
		{
			name: "TUI launch wiring without journey",
			files: changed(
				"cmd/agent-marketplace/tui.go", "cmd/agent-marketplace/sync.go",
				"cmd/agent-marketplace/tui_lifecycle_test.go", "cmd/other/tui.go", delta,
			),
			wantKinds:  []string{kindMissingTUIQAJourney},
			wantDetail: "cmd/agent-marketplace/tui.go",
			notDetail:  []string{"sync.go", "cmd/other"},
		},
		{
			name:        "non-Go tui files are not launch wiring",
			files:       changed("cmd/agent-marketplace/tui.yaml", "cmd/agent-marketplace/tuistate.json", delta),
			wantSummary: "no TUI-bearing changes",
		},
		// spec: spec-governance/tui-change-with-journey-passes
		{
			name:        "TUI change with PTY journey",
			files:       changed("internal/tui/app.go", "tools/tui-qa/tests/new-flow.spec.ts", delta),
			wantSummary: "1 TUI-bearing files with TUI QA journey",
		},
		{
			name:  "TUI launch wiring with node journey",
			files: changed("cmd/agent-marketplace/tui.go", "tools/tui-qa/tests/non-tty.test.ts", delta),
		},
		// spec: spec-governance/deleted-journey-does-not-count
		{
			name:      "deleted journey",
			files:     append(changed("internal/tui/app.go", delta), changedFile{path: "tools/tui-qa/tests/old-flow.spec.ts", deleted: true}),
			wantKinds: []string{kindMissingTUIQAJourney},
		},
		// spec: spec-governance/journey-helpers-do-not-count
		{
			name: "helpers, fixtures, README, and misplaced specs",
			files: changed(
				"internal/tui/app.go", "tools/tui-qa/tests/helpers.ts", "tools/tui-qa/tests/fixture.ts",
				"tools/tui-qa/README.md", "tools/tui-qa/package.json", "tools/tui-qa/flow.spec.ts", delta,
			),
			wantKinds: []string{kindMissingTUIQAJourney},
		},
		// spec: spec-governance/nested-journeys-do-not-count
		{
			name: "journeys nested below the suite directory",
			files: changed(
				"internal/tui/app.go", "tools/tui-qa/tests/flows/new-flow.spec.ts",
				"tools/tui-qa/tests/flows/process.test.ts", delta,
			),
			wantKinds: []string{kindMissingTUIQAJourney},
		},
		// spec: spec-governance/tui-test-only-change-needs-no-journey
		{
			name: "TUI tests, markdown, and testdata only",
			files: changed(
				"internal/tui/app_test.go", "internal/tui/README.md",
				"internal/tui/testdata/golden/list.txt", "cmd/agent-marketplace/tui_workspace_test.go",
			),
			wantSummary: "no TUI-bearing changes",
		},
		// spec: spec-governance/tui-qa-trailer-with-reason-passes
		{
			name:        "TUI-QA-Impact with em dash reason",
			files:       changed("internal/tui/app.go", delta),
			trailers:    map[string][]string{"TUI-QA-Impact": {"", "none — comment-only change"}},
			wantSummary: "TUI-QA-Impact: none — comment-only change",
		},
		{
			name:        "TUI-QA-Impact with ASCII dash reason",
			files:       changed("internal/tui/app.go", delta),
			trailers:    map[string][]string{"TUI-QA-Impact": {"none - rename only"}},
			wantSummary: "rename only",
		},
		// spec: spec-governance/tui-qa-trailer-without-reason-fails
		{
			name:       "TUI-QA-Impact without reason",
			files:      changed("internal/tui/app.go", delta),
			trailers:   map[string][]string{"TUI-QA-Impact": {"none", "", "none —  "}},
			wantKinds:  []string{kindTUIQAImpactNoReason},
			wantDetail: "TUI-QA-Impact: none — <reason>",
		},
		// spec: spec-governance/impact-trailers-are-not-interchangeable
		{
			name:      "Spec-Impact does not excuse a missing journey",
			files:     changed("internal/tui/app.go"),
			trailers:  map[string][]string{"Spec-Impact": {"none — refactor"}},
			wantKinds: []string{kindMissingTUIQAJourney},
		},
		{
			name:      "TUI-QA-Impact does not excuse a missing spec delta",
			files:     changed("internal/tui/app.go"),
			trailers:  map[string][]string{"TUI-QA-Impact": {"none — refactor"}},
			wantKinds: []string{kindMissingSpecDelta},
		},
		{
			name:      "both obligations missing",
			files:     changed("internal/tui/app.go"),
			wantKinds: []string{kindMissingSpecDelta, kindMissingTUIQAJourney},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, summary := evaluateGate(tc.files, tc.trailers)
			if got := kinds(findings); !slices.Equal(got, tc.wantKinds) {
				t.Fatalf("kinds = %v, want %v\n%s", got, tc.wantKinds, details(findings))
			}
			if !strings.Contains(summary, tc.wantSummary) {
				t.Errorf("summary %q does not contain %q", summary, tc.wantSummary)
			}
			if tc.wantDetail != "" && !strings.Contains(details(findings), tc.wantDetail) {
				t.Errorf("findings do not mention %q:\n%s", tc.wantDetail, details(findings))
			}
			for _, absent := range tc.notDetail {
				if strings.Contains(details(findings), absent) {
					t.Errorf("findings mention %q:\n%s", absent, details(findings))
				}
			}
		})
	}
}

// gitRepo creates an isolated repository and returns its root, a git runner,
// and a helper that writes one file and commits it with the given message.
func gitRepo(t *testing.T) (string, func(...string) string, func(rel, msg string) string) {
	t.Helper()
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
	return root, run, commit
}

func TestGateReadsGitRange(t *testing.T) {
	root, _, commit := gitRepo(t)
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

func TestGateReadsTUIQAEvidenceFromGitRange(t *testing.T) {
	root, run, commit := gitRepo(t)
	base := commit("tools/tui-qa/tests/old-flow.spec.ts", "init")
	check := func(from, to string, wantKinds []string, wantSummary string) {
		t.Helper()
		findings, summary, err := gate(root, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if got := kinds(findings); !slices.Equal(got, wantKinds) {
			t.Fatalf("kinds = %v, want %v\n%s", got, wantKinds, details(findings))
		}
		if !strings.Contains(summary, wantSummary) {
			t.Fatalf("summary %q does not contain %q", summary, wantSummary)
		}
	}

	tuiChange := commit("internal/tui/app.go", "change footer\n\nSpec-Impact: none — copy only")
	check(base, tuiChange, []string{kindMissingTUIQAJourney}, "")

	run("rm", "-q", "tools/tui-qa/tests/old-flow.spec.ts")
	run("commit", "-q", "-m", "drop journey")
	check(base, run("rev-parse", "HEAD"), []string{kindMissingTUIQAJourney}, "")

	journey := commit("tools/tui-qa/tests/new-flow.spec.ts", "prove footer")
	check(base, journey, nil, "1 TUI-bearing files with TUI QA journey")

	trailed := commit("internal/tui/footer.go", "reword comment\n\nSpec-Impact: none — comment only\nTUI-QA-Impact: none — comment-only change")
	check(journey, trailed, nil, "TUI-QA-Impact: none — comment-only change")
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
