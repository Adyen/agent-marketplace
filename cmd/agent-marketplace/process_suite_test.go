package main

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandProcessNestedPatterns(t *testing.T) {
	for _, tc := range []struct {
		selector string
		filter   string
		want     string
	}{
		{"TestOne", "TestOne/case", "^TestOne$/case"},
		{"TestCommandIntegration/Backup", "", "^TestCommandIntegration$/^Backup$"},
		{"TestCommandIntegration/Backup", "^TestCommandIntegration$/^Backup$/^a\\[1\\]$", "^TestCommandIntegration$/^Backup$/^a\\[1\\]$"},
		{"TestCommandIntegration/Backup", "TestCommandIntegration/(Backup|Sync)/case", "^TestCommandIntegration$/^Backup$/case"},
		{"TestCommandIntegration/Backup", "TestCommandIntegration/[B/]ackup/case", "^TestCommandIntegration$/^Backup$/case"},
		{"TestCommandIntegration/Backup", "Other/x|TestCommandIntegration/Backup/a|TestCommandIntegration/Backup/b", "^TestCommandIntegration$/^Backup$/a|^TestCommandIntegration$/^Backup$/b"},
		{"TestCommandIntegration/Backup", "TestCommandIntegration", "^TestCommandIntegration$/^Backup$"},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			got, err := commandScenarioPattern(tc.selector, tc.filter)
			if err != nil || got != tc.want {
				t.Fatalf("pattern = %q, err = %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, filter := range []string{"[", "Other/Backup"} {
		if _, err := commandScenarioPattern("TestCommandIntegration/Backup", filter); err == nil {
			t.Fatalf("invalid or nonmatching filter accepted: %q", filter)
		}
	}
}

func TestCommandProcessRegisteredSelectors(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		run  string
		skip string
	}{
		{"exact", "^TestCommandIntegration$/^ProcessIsolation$/^a\\[1\\]$", ""},
		{"alternation", "Other/none|^TestCommandIntegration$/^ProcessIsolation$/^a\\[1\\]$", ""},
		{"skip", "^TestCommandIntegration$/^ProcessIsolation$", "^TestCommandIntegration$/^ProcessIsolation$/^a1$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), executable, "-test.run="+tc.run, "-test.skip="+tc.skip, "-test.count=2", "-test.shuffle=123", "-test.v", "-test.timeout=30s")
			output, err := cmd.CombinedOutput()
			if err != nil || strings.Count(string(output), "selected-suite-probe") != 2 || strings.Contains(string(output), "unselected-suite-probe") {
				t.Fatalf("registered selection failed: %v\n%s", err, output)
			}
		})
	}
}

func TestCommandProcessRegistrationGuard(t *testing.T) {
	for _, tc := range []struct {
		name        string
		filename    string
		declaration string
		violations  int
	}{
		{"registered", "new_test.go", "func scenarioKnown(t *testing.T) {}", 0},
		{"unregistered", "new_test.go", "func scenarioForgotten(t *testing.T) {}", 1},
		{"top-level bypass", "new_test.go", "func TestForgotten(t *testing.T) {}", 1},
		{"pure unit", "version_test.go", "func TestVersion(t *testing.T) {}", 0},
		{"policy", "policy_test.go", "func TestRepositoryIgnoresDependencyTransactionLockFiles(t *testing.T) {}", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), tc.filename, "package main\n"+tc.declaration, 0)
			if err != nil {
				t.Fatal(err)
			}
			violations := commandRegistrationViolations(tc.filename, file, map[string]bool{"Known": true})
			if len(violations) != tc.violations {
				t.Fatalf("violations = %v, want %d", violations, tc.violations)
			}
		})
	}
}

func scenarioProcessIsolation(t *testing.T) {
	selector := os.Getenv(processIsolatedSelectorEnv)
	if *commandTestsInProcess || testing.CoverMode() != "" {
		if selector != "" {
			t.Fatal("serial or coverage scenario entered a child")
		}
	} else if selector != t.Name() || os.Getenv(processIsolatedRootEnv) == "" {
		t.Fatalf("scenario bypassed process isolation: selector=%q", selector)
	}
	t.Run("a[1]", func(t *testing.T) { t.Log("selected-suite-probe") })
	t.Run("a1", func(t *testing.T) { t.Log("unselected-suite-probe") })
}
