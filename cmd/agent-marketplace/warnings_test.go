package main

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type testResourceWarning struct {
	resource string
	message  string
	dedupKey string
}

type testEmptyDedupWarning struct {
	resource string
	message  string
}

func (w testEmptyDedupWarning) Error() string    { return w.message }
func (w testEmptyDedupWarning) Resource() string { return w.resource }
func (w testEmptyDedupWarning) DedupKey() string { return "" }

type testRemediableResourceWarning struct {
	testResourceWarning
	remediation string
}

func (w testRemediableResourceWarning) Remediation() string { return w.remediation }

func (w testResourceWarning) Error() string {
	if w.message != "" {
		return w.message
	}
	return "resource was dropped"
}
func (w testResourceWarning) Resource() string { return w.resource }
func (w testResourceWarning) DedupKey() string {
	if w.dedupKey != "" {
		return w.dedupKey
	}
	return "drop\x00" + w.resource
}

func TestWriteGroupedWarningsRollsUpResources(t *testing.T) {
	warnings := make([]error, 0, 7)
	for i := 1; i <= 7; i++ {
		warnings = append(warnings, testResourceWarning{resource: fmt.Sprintf("resource-%d", i)})
	}

	var out bytes.Buffer
	if err := writeGroupedWarnings(&out, warnings, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Count(got, "warning: resource was dropped") != 1 {
		t.Fatalf("warning was not rolled up:\n%s", got)
	}
	if !strings.Contains(got, "7 resources: resource-1, resource-2, resource-3, resource-4, resource-5 (and 2 more)") {
		t.Fatalf("resource summary missing:\n%s", got)
	}
}

func TestWriteGroupedWarningsGroupsByDedupKey(t *testing.T) {
	warnings := []error{
		testResourceWarning{resource: "resource-a", message: "resource-a was dropped", dedupKey: "drop"},
		testResourceWarning{resource: "resource-b", message: "resource-b was dropped", dedupKey: "drop"},
	}

	var out bytes.Buffer
	if err := writeGroupedWarnings(&out, warnings, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Count(got, "warning:") != 1 {
		t.Fatalf("warnings were not grouped by dedup key:\n%s", got)
	}
	if !strings.Contains(got, "2 resources: resource-a, resource-b") {
		t.Fatalf("resource summary missing:\n%s", got)
	}
}

func TestWriteGroupedWarningsSkipsNilWarnings(t *testing.T) {
	warnings := []error{nil, testResourceWarning{resource: "resource-a"}}

	var out bytes.Buffer
	if err := writeGroupedWarnings(&out, warnings, false); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "warning: resource was dropped\n" {
		t.Fatalf("writeGroupedWarnings() = %q, want only the non-nil warning", got)
	}
}

func TestWriteGroupedWarningsDoesNotGroupEmptyDedupKeys(t *testing.T) {
	warnings := []error{
		testEmptyDedupWarning{resource: "resource-a", message: "first warning"},
		testEmptyDedupWarning{resource: "resource-b", message: "second warning"},
	}

	var out bytes.Buffer
	if err := writeGroupedWarnings(&out, warnings, false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out.String(), "warning:"); got != 2 {
		t.Fatalf("writeGroupedWarnings() printed %d warnings, want 2:\n%s", got, out.String())
	}
}

func TestWriteGroupedWarningsIncludesRemediationForResourceGroup(t *testing.T) {
	warnings := []error{
		testRemediableResourceWarning{
			testResourceWarning: testResourceWarning{resource: "resource-a", message: "resource-a warning", dedupKey: "shared"},
			remediation:         "fix the resources",
		},
		testRemediableResourceWarning{
			testResourceWarning: testResourceWarning{resource: "resource-b", message: "resource-b warning", dedupKey: "shared"},
			remediation:         "fix the resources",
		},
	}

	var out bytes.Buffer
	if err := writeGroupedWarnings(&out, warnings, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "2 resources: resource-a, resource-b") {
		t.Fatalf("resource summary missing:\n%s", got)
	}
	if !strings.Contains(got, "→ fix the resources") {
		t.Fatalf("remediation missing:\n%s", got)
	}
}

func TestWriteSyncIssuesGroupsResolverWarningsByProblem(t *testing.T) {
	issues := []error{
		&resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "alpha", URL: "registry-a", Ref: "main"},
		&resolver.SelectedNotFoundError{Kind: ir.KindCommand, Name: "beta", URL: "registry-b", Ref: "main"},
		&resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: "gamma", Winner: "registry-a", RunnersUp: []string{"registry-b"}},
		&resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: "delta", Winner: "registry-b", RunnersUp: []string{"registry-c"}},
	}

	var out bytes.Buffer
	if err := writeSyncIssues(&out, issues, false, "warning:"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if count := strings.Count(got, "warning: "); count != 2 {
		t.Fatalf("resolver warnings rendered as %d top-level problems, want 2 grouped problems:\n%s", count, got)
	}
	for _, want := range []string{"selected resources were not found (2 resources)", "resources are provided by multiple registries (2 resources)", `skill "alpha"`, `command "beta"`, `skill "gamma"`, `skill "delta"`} {
		if !strings.Contains(got, want) {
			t.Errorf("grouped resolver warning is missing %q:\n%s", want, got)
		}
	}

	var verbose bytes.Buffer
	if err := writeSyncIssues(&verbose, issues, true, "warning:"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`skill "alpha" (registry-a@main)`, `skill "gamma" (using registry-a (others: registry-b))`} {
		if !strings.Contains(verbose.String(), want) {
			t.Errorf("verbose grouped warning is missing %q:\n%s", want, verbose.String())
		}
	}

	errors := []error{
		&resolver.NameCollisionError{Kind: ir.KindSkill, Name: "gamma", Sources: []string{"registry-a", "registry-b"}},
		&resolver.NameCollisionError{Kind: ir.KindCommand, Name: "delta", Sources: []string{"registry-b", "registry-c"}},
	}
	errorOutput := newSyncCommandError(errors, false, "error:").Error()
	if count := strings.Count(errorOutput, "error: "); count != 1 {
		t.Fatalf("resolver errors rendered as %d top-level problems, want 1 grouped problem:\n%s", count, errorOutput)
	}
	if !strings.Contains(errorOutput, "resources are provided by multiple registries (2 resources)") {
		t.Fatalf("grouped resolver error is missing its category and count:\n%s", errorOutput)
	}
}

func TestWriteSyncIssuesCompactsLockfileWarnings(t *testing.T) {
	issues := []error{
		sourcerevision.ResourceWarning{Kind: "rule", Name: "linting", Reason: "unsupported resource kind"},
		sourcerevision.ResourceWarning{Kind: "rule", Name: "testing", Reason: "unsupported resource kind"},
		&catalog.OverlappingEntryWarning{Dir: "/snapshot/plugin-a", FirstURL: "registry", FirstPath: "plugin-a", SecondURL: "registry", SecondPath: "plugin-a"},
		&catalog.OverlappingEntryWarning{Dir: "/snapshot/plugin-b", FirstURL: "registry", FirstPath: "plugin-b", SecondURL: "registry", SecondPath: "plugin-b"},
	}

	var out bytes.Buffer
	if err := writeSyncIssues(&out, issues, false, "warning:"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if count := strings.Count(got, "warning: "); count != 2 {
		t.Fatalf("lockfile warnings rendered as %d top-level problems, want 2:\n%s", count, got)
	}
	for _, want := range []string{
		"source revision omitted resources: unsupported resource kind (2 resources)",
		"registry entries overlap (2 resources)",
		"rule/linting", "rule/testing", "plugin-a", "plugin-b",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compacted warning output is missing %q:\n%s", want, got)
		}
	}
}

func TestWriteSyncIssuesGroupsDistinctProblemsByDiagnosticCode(t *testing.T) {
	issues := []error{
		pipeline.PromoteDiagnostic(diag.Diagnostic{
			Code: diag.CodeSkillDevinToolUnknown, Resource: "alpha", Message: "first scoped tool is unsupported", Hint: "remove command scopes",
		}),
		pipeline.PromoteDiagnostic(diag.Diagnostic{
			Code: diag.CodeSkillDevinToolUnknown, Resource: "beta", Message: "second scoped tool is unsupported", Hint: "remove command scopes",
		}),
		pipeline.PromoteDiagnostic(diag.Diagnostic{
			Code: diag.CodeSkillDevinToolUnknown, Resource: "gamma", Message: "the tool has no Devin equivalent", Hint: "choose a supported tool",
		}),
	}

	var warnings bytes.Buffer
	if err := writeSyncIssues(&warnings, issues, false, "warning:"); err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"warnings": warnings.String(),
		"errors":   newSyncCommandError(issues, false, "error:").Error(),
	}
	for name, got := range outputs {
		t.Run(name, func(t *testing.T) {
			if count := strings.Count(got, diag.CodeSkillDevinToolUnknown); count != 1 {
				t.Fatalf("diagnostic code rendered %d times, want one code group:\n%s", count, got)
			}
			if !strings.Contains(got, "(3 problems, 3 resources)") {
				t.Fatalf("diagnostic code group is missing its problem and resource counts:\n%s", got)
			}
			for _, want := range []string{"first scoped tool is unsupported", "second scoped tool is unsupported", "the tool has no Devin equivalent", "alpha", "beta", "gamma", "remove command scopes", "choose a supported tool"} {
				if strings.Count(got, want) != 1 {
					t.Errorf("grouped diagnostic output should contain %q exactly once:\n%s", want, got)
				}
			}
		})
	}
}

// spec: resource-prerequisites/remediation-with-hints
func TestWriteGroupedWarningsIncludesRequirementRemediation(t *testing.T) {
	warning := &resolver.UnmetRequirementWarning{
		ResourceKind: ir.KindSkill,
		ResourceName: "smoke-test-kb",
		Requirement: catalog.Requirement{
			Kind:     ir.KindMcpServer,
			Name:     "genai-atlassian",
			Registry: "git@example.com:agents/registry.git",
			Path:     "integrations",
		},
	}

	var out bytes.Buffer
	if err := writeGroupedWarnings(&out, []error{warning}, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `agent-marketplace install "genai-atlassian" --kind mcp --from "git@example.com:agents/registry.git" --path "integrations"`) {
		t.Fatalf("requirement remediation missing:\n%s", got)
	}
}

// spec: resource-prerequisites/rows-are-sorted
func TestSplitRequirementWarningsSortsRows(t *testing.T) {
	newWarning := func(resourceKind ir.ResourceKind, resourceName string, reqKind ir.ResourceKind, reqName string) error {
		return &resolver.UnmetRequirementWarning{
			ResourceKind: resourceKind,
			ResourceName: resourceName,
			Requirement:  catalog.Requirement{Kind: reqKind, Name: reqName},
		}
	}
	errs := []error{
		newWarning(ir.KindSkill, "zulu", ir.KindMcpServer, "second-mcp"),
		newWarning(ir.KindSkill, "alpha", ir.KindMcpServer, "docs"),
		newWarning(ir.KindSkill, "zulu", ir.KindSkill, "first-skill"),
	}

	rows, other := splitRequirementWarnings(errs)
	if len(other) != 0 {
		t.Fatalf("splitRequirementWarnings() non-requirement errors = %v, want none", other)
	}
	got := make([][2]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, [2]string{row.Resource, row.Requirement})
	}
	want := [][2]string{
		{"skill:alpha", "mcp:docs"},
		{"skill:zulu", "mcp:second-mcp"},
		{"skill:zulu", "skill:first-skill"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitRequirementWarnings() rows = %v, want %v", got, want)
	}
}

func TestCollisionWarningDisplayIsTerminalSafe(t *testing.T) {
	var out bytes.Buffer
	warning := &resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: "review", Winner: "WIN\x1b[31m", RunnersUp: []string{"OTHER\x1b[31m"}, WinnerURL: "https://fixture:FAKE_SECRET@example.invalid/repo?token=FAKE_TOKEN"}
	if err := writeGroupedWarnings(&out, []error{warning}, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"multiple registries", `\x1b[31m`, "agent-marketplace remove", "--from"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.ContainsAny(text, "\x1b") || strings.Contains(text, "FAKE_SECRET") || strings.Contains(text, "FAKE_TOKEN") {
		t.Fatalf("unsafe collision warning: %q", text)
	}
}
