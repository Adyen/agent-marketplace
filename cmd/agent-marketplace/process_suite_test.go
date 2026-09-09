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
		{"TestBackup", "", "^TestBackup$"},
		{"TestBackup", "^TestBackup$/^a\\[1\\]$", "^TestBackup$/^a\\[1\\]$"},
		{"TestBackup", "Test(Backup|Sync)/case", "^TestBackup$/case"},
		{"TestBackup", "Test[B/]ackup/case", "^TestBackup$/case"},
		{"TestBackup", "Other/x|TestBackup/a|TestBackup/b", "^TestBackup$/a|^TestBackup$/b"},
		{"TestBackup", "Backup", "^TestBackup$"},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			got, err := commandScenarioPattern(tc.selector, tc.filter)
			if err != nil || got != tc.want {
				t.Fatalf("pattern = %q, err = %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, filter := range []string{"[", "Other/Backup"} {
		if _, err := commandScenarioPattern("TestBackup", filter); err == nil {
			t.Fatalf("invalid or nonmatching filter accepted: %q", filter)
		}
	}
}

func TestCommandProcessTopLevelSelectors(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		run  string
		skip string
	}{
		{"exact", "^TestProcessIsolation$/^a\\[1\\]$", ""},
		{"alternation", "Other/none|^TestProcessIsolation$/^a\\[1\\]$", ""},
		{"skip", "^TestProcessIsolation$", "^TestProcessIsolation$/^a1$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), executable, "-test.run="+tc.run, "-test.skip="+tc.skip, "-test.count=2", "-test.shuffle=123", "-test.v", "-test.timeout=30s")
			output, err := cmd.CombinedOutput()
			if err != nil || strings.Count(string(output), "selected-suite-probe") != 2 || strings.Contains(string(output), "unselected-suite-probe") {
				t.Fatalf("top-level selection failed: %v\n%s", err, output)
			}
		})
	}
}

func TestCommandProcessIsolationGuard(t *testing.T) {
	const guard = "\tif isolateCommandTest(t) {\n\t\treturn\n\t}\n"
	for _, tc := range []struct {
		name        string
		filename    string
		declaration string
		violations  int
	}{
		{"guarded", "new_test.go", "func TestKnown(t *testing.T) {\n" + guard + "}", 0},
		{"unguarded", "new_test.go", "func TestForgotten(t *testing.T) {}", 1},
		{"guard not first", "new_test.go", "func TestLate(t *testing.T) {\n\tt.Log(1)\n" + guard + "}", 1},
		{"guard without return", "new_test.go", "func TestNoReturn(t *testing.T) {\n\tif isolateCommandTest(t) {\n\t\tt.Log(1)\n\t}\n}", 1},
		{"helper", "new_test.go", "func testHelper(t *testing.T) {}", 0},
		{"pure unit file", "version_test.go", "func TestVersion(t *testing.T) {}", 0},
		{"policy", "policy_test.go", "func TestRepositoryIgnoresDependencyTransactionLockFiles(t *testing.T) {}", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), tc.filename, "package main\n"+tc.declaration, 0)
			if err != nil {
				t.Fatal(err)
			}
			violations := commandIsolationViolations(tc.filename, file)
			if len(violations) != tc.violations {
				t.Fatalf("violations = %v, want %d", violations, tc.violations)
			}
		})
	}
}

func TestProcessIsolation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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
