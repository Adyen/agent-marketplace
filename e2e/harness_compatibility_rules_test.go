package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func assertCompatibilityRules(t *testing.T, e *Env, f compatibilityFixture, binary string, row *compatibilityResult) {
	t.Helper()
	source := filepath.Join(f.registry, "rule-source")
	output := filepath.Join(e.repos, "converted-rule")
	captureCompatibilityFile(t, e, row, filepath.Join(source, ".factory", "rules", f.name("rule")+".md"), f.name("rule-body"))
	command := runCompatibilityCommand(e, binary, []string{source, row.Harness}, 20*time.Second)
	row.Commands = append(row.Commands, command)
	if command.Error != "" {
		t.Fatal(command.Error)
	}
	var result struct {
		Artifacts   []struct{ Path string }
		Diagnostics []struct {
			Code     string
			Severity int
			Resource string
		}
	}
	if err := json.Unmarshal([]byte(command.output), &result); err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == 0 {
			t.Fatalf("rule compiler returned error diagnostic %s", diagnostic.Code)
		}
	}
	convert := runCompatibilityCommand(e, e.binary, []string{"convert", "--from", "factory", "--to", row.Harness, "--out", output, source}, 20*time.Second)
	row.Commands = append(row.Commands, convert)
	if convert.Error != "" {
		t.Fatal(convert.Error)
	}
	unsupported := map[string]string{
		"claude": "rule.claude.no-primitive",
		"gemini": "rule.gemini.no-primitive",
		"codex":  "rule.codex.unsupported",
	}
	if code := unsupported[row.Harness]; code != "" {
		found := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == code && diagnostic.Severity == 1 && diagnostic.Resource == f.name("rule") {
				found = true
			}
		}
		if !found || len(result.Artifacts) != 0 {
			t.Fatalf("expected %s warning for this rule and no artifacts; got %+v", code, result)
		}
		if err := filepath.WalkDir(output, func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) && path == output {
				return nil
			}
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				t.Errorf("unsupported rule conversion emitted a file: %s", path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		row.Status = "unsupported-as-expected"
		row.Reason = code + ": typed compiler warning and convert output absence asserted; rules are not selectable through the hosted Marketplace API"
		return
	}
	path := map[string]string{
		"factory":  "rules/" + f.name("rule") + ".md",
		"cursor":   "rules/" + f.name("rule") + ".mdc",
		"windsurf": ".windsurf/rules/" + f.name("rule") + ".md",
		"devin":    "rules/" + f.name("rule") + ".md",
	}[row.Harness]
	if len(result.Artifacts) != 1 || result.Artifacts[0].Path != path {
		t.Fatalf("wrong rule compiler output: %+v", result.Artifacts)
	}
	captureCompatibilityFile(t, e, row, filepath.Join(output, filepath.FromSlash(path)), f.name("rule-body"))
	row.Reason = "scope-neutral compiler and Marketplace convert path/body verified from a rule-only Factory source; not installed through Marketplace, not natively inspected"
}
