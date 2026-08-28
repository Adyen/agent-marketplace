package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
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

func TestWriteSyncIssuesGroupsSelectedNotFoundWarnings(t *testing.T) {
	issues := []error{
		&resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "alpha", URL: "registry-a", Ref: "main"},
		&resolver.SelectedNotFoundError{Kind: ir.KindCommand, Name: "beta", URL: "registry-b", Ref: "main"},
	}

	var out bytes.Buffer
	if err := writeSyncWarnings(&out, issues, false, "warning:"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Count(got, "warning:") != 1 {
		t.Fatalf("selected-not-found warnings rendered separately:\n%s", got)
	}
	for _, want := range []string{"selected resources were not found (2 resources)", `skill "alpha"`, `command "beta"`} {
		if !strings.Contains(got, want) {
			t.Errorf("grouped warning is missing %q:\n%s", want, got)
		}
	}
}

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
