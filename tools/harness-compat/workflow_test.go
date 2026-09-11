package main

import (
	"os"
	"os/exec"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRequiredCompatibilityGateRejectsIncompleteRuns(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/harness-compatibility.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On   map[string]yaml.Node `yaml:"on"`
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var gate struct {
		Name        string         `yaml:"name"`
		If          string         `yaml:"if"`
		Needs       []string       `yaml:"needs"`
		Permissions map[string]any `yaml:"permissions"`
		Steps       []struct {
			Run string            `yaml:"run"`
			Env map[string]string `yaml:"env"`
		} `yaml:"steps"`
	}
	node, exists := workflow.Jobs["required"]
	if !exists {
		t.Fatal("required compatibility gate is missing")
	}
	if err := node.Decode(&gate); err != nil {
		t.Fatal(err)
	}
	if gate.Name != "Harness Compatibility" || gate.If != "always()" || !slices.Equal(gate.Needs, []string{"plan", "contracts", "probe"}) {
		t.Fatal("stable always-running gate must depend on planning, contracts, and the entire probe matrix")
	}
	if gate.Permissions == nil || len(gate.Permissions) != 0 || len(gate.Steps) != 1 || gate.Steps[0].Run == "" {
		t.Fatal("required gate must be an unprivileged result check")
	}
	if _, ok := workflow.On["pull_request"]; !ok {
		t.Fatal("required gate must run for pull requests")
	}
	step := gate.Steps[0]
	variables := []string{"PLAN_RESULT", "CONTRACTS_RESULT", "PROBES_RESULT"}
	for i, job := range []string{"plan", "contracts", "probe"} {
		if step.Env[variables[i]] != "${{ needs."+job+".result }}" {
			t.Fatalf("gate does not inspect %s's actual job result", job)
		}
	}
	run := func(t *testing.T, results []string, accepted bool) {
		t.Helper()
		cmd := exec.Command("bash", "-c", step.Run)
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		for i, variable := range variables {
			cmd.Env = append(cmd.Env, variable+"="+results[i])
		}
		output, err := cmd.CombinedOutput()
		if (err == nil) != accepted {
			t.Fatalf("gate acceptance = %t, want %t for %v: %s", err == nil, accepted, results, output)
		}
	}
	t.Run("all-success", func(t *testing.T) { run(t, []string{"success", "success", "success"}, true) })
	for i, upstream := range []string{"plan", "contracts", "probe"} {
		for _, outcome := range []string{"failure", "cancelled", "skipped", ""} {
			t.Run(upstream+"/"+outcome, func(t *testing.T) {
				results := []string{"success", "success", "success"}
				results[i] = outcome
				run(t, results, false)
			})
		}
	}
}
