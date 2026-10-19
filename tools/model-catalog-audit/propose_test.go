package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const proposeSourceURL = "https://platform.claude.com/docs/en/about-claude/models/overview.md"

func writeProposeTree(t *testing.T, catalogs map[string]string) string {
	t.Helper()
	root := t.TempDir()
	var lines []string
	for i, s := range sources {
		comma := ","
		if i == len(sources)-1 {
			comma = ""
		}
		lines = append(lines, fmt.Sprintf(`  %q: {"checked": "2026-09-23", "sha256": "%s"}%s`, s.Name, digest([]byte(s.Name)), comma))
	}
	files := map[string]string{baselinesPath: "{\n" + strings.Join(lines, "\n") + "\n}\n"}
	for harness, body := range catalogs {
		files[catalogPath(harness)] = body
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readProposeFile(t *testing.T, root, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func proposeReport(sources ...sourceReport) report {
	return report{Schema: 1, CheckedAt: "2026-10-19T05:04:13Z", Sources: sources}
}

func TestProposeRefreshesBaselinesAndSkipsUnusableDigests(t *testing.T) {
	root := writeProposeTree(t, nil)
	before := readProposeFile(t, root, baselinesPath)
	newDigest := digest([]byte("new-claude"))
	r := proposeReport(
		sourceReport{Name: "claude-subagents", Status: "drift", ObservedSHA256: newDigest, Baseline: baseline{SHA256: digest([]byte("claude-subagents"))}, URL: "https://code.claude.com/docs/en/sub-agents.md", Findings: []finding{{Code: "source.evidence-changed"}}},
		sourceReport{Name: "codex-models", Status: "unknown", ObservedSHA256: newDigest, Findings: []finding{{Code: "source.evidence-changed"}}},
		sourceReport{Name: "cursor-models", Status: "drift", ObservedSHA256: "NOT-HEX", Findings: []finding{{Code: "source.evidence-changed"}}},
	)
	var out bytes.Buffer
	if err := run([]string{"propose", "--root", root}, strings.NewReader(mustJSON(t, r)), &out, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	after := readProposeFile(t, root, baselinesPath)
	want := fmt.Sprintf(`  "claude-subagents": {"checked": "2026-10-19", "sha256": "%s"},`, newDigest)
	if !strings.Contains(after, want) {
		t.Fatalf("baseline line not refreshed:\n%s", after)
	}
	beforeLines, afterLines := strings.Split(before, "\n"), strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("line count changed: %d -> %d", len(beforeLines), len(afterLines))
	}
	changed := 0
	for i := range beforeLines {
		if beforeLines[i] != afterLines[i] {
			changed++
		}
	}
	if changed != 1 || !strings.Contains(out.String(), "**cursor-models**: observed digest is not") || !strings.Contains(out.String(), "**codex-models**: no freshness conclusion") {
		t.Fatalf("changed lines = %d, output:\n%s", changed, out.String())
	}
}

func TestProposeReusesIdentityDerivesProviderAndListsManualCases(t *testing.T) {
	const claudeCatalog = `schema: 1
harness: claude
surfaces:
  subagent:
    model_field: model
models:
  anthropic/claude-opus-5:
    model: claude-opus-5
families:
  anthropic/opus:
    pin: anthropic/claude-opus-5
`
	const factoryCatalog = `schema: 1
harness: factory
surfaces:
  subagent:
    model_field: model
models:
  anthropic/claude-fable-5:
    model: claude-fable-5
    reasoning: {low: {effort: low}, medium: {effort: medium}, high: {effort: high}}
  anthropic/claude-haiku-5-5:
    model: claude-haiku-5-5
  openai/gpt-6-sol:
    model: gpt-6-sol
  meta/gemma-2:
    model: gemma-2
`
	const geminiCatalog = `schema: 1
harness: gemini
surfaces:
  subagent:
    model_field: model
models:
  google/gemini-3-pro:
    model: gemini-3-pro
  google/gemma-3:
    model: gemma-3

# Families are reviewed separately.
families:
  google/gemini:
    pin: google/gemini-3-pro
`
	const devinCatalog = `schema: 1
harness: devin
surfaces:
  subagent:
    model_field: model
models:
  google/gemini-3.8-flash:
    model: gemini-3.8-flash
`
	root := writeProposeTree(t, map[string]string{"claude": claudeCatalog, "factory": factoryCatalog, "gemini": geminiCatalog, "devin": devinCatalog})
	claudeURL := proposeSourceURL
	r := proposeReport(
		sourceReport{Name: "claude-models", Harness: "claude", URL: claudeURL, Status: "drift", Findings: []finding{
			{Code: "catalog.model-missing", Model: "claude-sonnet-5-5"},
			{Code: "catalog.model-missing", Model: "claude-haiku-5-5"},
			{Code: "catalog.model-missing", Model: "claude-opus-5"},
			{Code: "catalog.model-not-observed", Model: "claude-sonnet-5"},
		}},
		sourceReport{Name: "hook-claude", Harness: "claude", CatalogDomain: "hookpolicy", Status: "drift", Findings: []finding{{Code: "catalog.model-missing", Model: "not-a-model-stub"}}},
		sourceReport{Name: "factory-models", Harness: "factory", URL: "https://docs.factory.ai/docs/models.md", Status: "drift", Observed: observation{Models: []observedModel{
			{ID: "gpt-6.1-sol", Efforts: []string{"high", "low", "max", "medium", "xhigh"}},
			{ID: "claude-sonnet-5-5", Efforts: []string{"high", "low", "off"}},
		}}, Findings: []finding{
			{Code: "catalog.model-missing", Model: "gpt-6.1-sol"},
			{Code: "catalog.model-missing", Model: "gpt-6.1-sol"},
			{Code: "catalog.model-missing", Model: "claude-sonnet-5-5"},
			{Code: "catalog.model-missing", Model: "mistral-large-4"},
			{Code: "catalog.model-missing", Model: "gemma-4-31b-it"},
		}},
		sourceReport{Name: "gemini-models", Harness: "gemini", URL: "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/core/src/config/models.ts", Status: "drift", Findings: []finding{
			{Code: "catalog.model-missing", Model: "gemini-3.8-flash"},
			{Code: "catalog.model-missing", Model: "gemini-2.5-pro"},
			{Code: "catalog.model-missing", Model: "gemma-4-31b-it"},
		}},
	)
	var out bytes.Buffer
	if err := run([]string{"propose", "--root", root}, strings.NewReader(mustJSON(t, r)), &out, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"## Model catalog drift proposal", "### Needs a manual catalog entry", "`mistral-large-4`", "`gemma-4-31b-it`", "### Collisions with existing keys (not changed)", "`anthropic/claude-opus-5`", "Reviewer checklist", "GITHUB_TOKEN"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "not-a-model-stub") || strings.Count(body, "`openai/gpt-6.1-sol`") != 1 {
		t.Fatalf("hook-domain finding or duplicate finding leaked into proposal:\n%s", body)
	}

	cs, err := loadCatalogs(os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	byHarness := map[string]catalog{}
	for _, c := range cs {
		byHarness[c.Harness] = c
	}
	for harness, key := range map[string]string{"claude": "anthropic/claude-sonnet-5-5", "factory": "anthropic/claude-sonnet-5-5", "gemini": "google/gemini-2.5-pro"} {
		if _, ok := byHarness[harness].Models[key]; !ok {
			t.Fatalf("%s catalog missing %s", harness, key)
		}
	}
	if _, ok := byHarness["claude"].Models["anthropic/claude-haiku-5-5"]; !ok {
		t.Fatal("claude catalog did not reuse the identity key from factory")
	}
	if _, ok := byHarness["gemini"].Models["google/gemini-3.8-flash"]; !ok {
		t.Fatal("gemini catalog did not reuse the identity key from devin")
	}
	if got := byHarness["factory"].Models["openai/gpt-6.1-sol"].Reasoning; len(got) != 3 {
		t.Fatalf("factory gpt reasoning = %v, want low/medium/high", got)
	}
	if got := byHarness["factory"].Models["anthropic/claude-sonnet-5-5"].Reasoning; len(got) != 2 || got["low"].Effort != "low" || got["high"].Effort != "high" || got["medium"].Effort != "" {
		t.Fatalf("factory reasoning must follow observed efforts [high low off], got %v", got)
	}
	if _, ok := byHarness["claude"].Models["anthropic/claude-sonnet-5-5"]; !ok || len(byHarness["claude"].Models["anthropic/claude-sonnet-5-5"].Reasoning) != 0 {
		t.Fatal("non-factory stub must not carry reasoning")
	}
	if n := strings.Count(readProposeFile(t, root, catalogPath("claude")), "anthropic/claude-opus-5:"); n != 1 {
		t.Fatalf("collision key was rewritten: %d occurrences", n)
	}
	if !strings.Contains(readProposeFile(t, root, catalogPath("claude")), "# TODO(model-catalog-drift): stub from "+claudeURL+"; verify native resource support and reasoning before merging.") {
		t.Fatal("stub is missing its TODO comment")
	}
}

func TestProposeWithoutChangesPrintsNothing(t *testing.T) {
	root := writeProposeTree(t, nil)
	before := readProposeFile(t, root, baselinesPath)
	r := proposeReport(sourceReport{Name: "claude-skills", Status: "no-drift"})
	var out bytes.Buffer
	if err := run([]string{"propose", "--root", root}, strings.NewReader(mustJSON(t, r)), &out, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || readProposeFile(t, root, baselinesPath) != before {
		t.Fatalf("no-change proposal wrote output %q or edited baselines", out.String())
	}
}

func TestInsertStubsBeforeNextTopLevelKeyAndAtEOF(t *testing.T) {
	stubs := []stub{
		{harness: "factory", key: "openai/gpt-z", model: "gpt-z", url: "https://x.example/models"},
		{harness: "factory", key: "openai/gpt-a", model: "gpt-a", url: "https://x.example/models", reasoning: []string{"low", "high"}},
	}
	const todo = "  # TODO(model-catalog-drift): stub from https://x.example/models; verify native resource support and reasoning before merging.\n"
	const aStub = "  openai/gpt-a:\n    model: gpt-a\n    reasoning: {low: {effort: low}, high: {effort: high}}\n"
	const zStub = "  openai/gpt-z:\n    model: gpt-z\n"
	for _, tt := range []struct{ name, body, want string }{
		{"before next key", "schema: 1\nmodels:\n  openai/old:\n    model: old\n\n# Families\nfamilies:\n  x:\n    pin: y\n",
			"schema: 1\nmodels:\n  openai/old:\n    model: old\n" + todo + aStub + todo + zStub + "\n# Families\nfamilies:\n  x:\n    pin: y\n"},
		{"at EOF", "schema: 1\nmodels:\n  openai/old:\n    model: old\n",
			"schema: 1\nmodels:\n  openai/old:\n    model: old\n" + todo + aStub + todo + zStub},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := insertStubs([]byte(tt.body), slices.Clone(stubs))
			if err != nil || string(got) != tt.want {
				t.Fatalf("insertStubs() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	if _, err := insertStubs([]byte("schema: 1\n"), stubs); err == nil {
		t.Fatal("missing models block must be an error")
	}
}

func TestProposeRejectsUnknownArguments(t *testing.T) {
	for _, args := range [][]string{{"propose", "extra"}, {"propose", "--update"}} {
		if err := run(args, strings.NewReader(""), &bytes.Buffer{}, nil, time.Now()); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
