package main

import (
	"os/exec"
	"path"
	"slices"
	"strings"
	"testing"
)

var expectedCategories = []string{
	"go_build",
	"go_test",
	"benchmark",
	"harness_compatibility",
	"marketplace_e2e",
	"tui_qa",
	"container_config",
	"container_marketplace",
	"container_web",
	"container_site",
	"helm",
}

var expectedBenchmarkRoots = []string{
	"internal/resolver",
	"internal/materialize",
	"internal/tui",
	"internal/catalog",
	"internal/config",
	"cmd/agent-marketplace",
	"internal/cache",
	"pkg/harness-compilation",
	"internal/dependencytxn",
	"internal/lockfile",
	"internal/sourcerevision",
	"internal/pipeline",
	"internal/marketplace",
	"internal/marketplacelock",
}

func want(trues ...string) map[string]bool {
	w := make(map[string]bool, len(expectedCategories))
	for _, c := range expectedCategories {
		w[c] = false
	}
	for _, c := range trues {
		w[c] = true
	}
	return w
}

func assertCategories(t *testing.T, got map[string]bool, w map[string]bool) {
	t.Helper()
	for _, name := range expectedCategories {
		if got[name] != w[name] {
			t.Errorf("%s = %v, want %v", name, got[name], w[name])
		}
	}
}

func TestPlanCategories(t *testing.T) {
	all := want(expectedCategories...)

	cases := []struct {
		name  string
		paths []string
		all   bool
		want  map[string]bool
	}{
		{"empty", []string{}, false, want()},
		{"--all", nil, true, all},
		{"docs-only", []string{"README.md", "docs/architecture.md"}, false, want()},
		{"product Go", []string{"internal/resolver/source.go"}, false, want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace")},
		{"unbenchmarked tool Go", []string{"tools/harness-compat/main.go"}, false, want("go_test", "harness_compatibility")},
		{"benchmark report", []string{"tools/benchmark-report/main.go"}, false, want("go_test", "benchmark")},
		{"E2E", []string{"e2e/marketplace_test.go"}, false, want("marketplace_e2e")},
		{"harness compatibility E2E", []string{"e2e/harness_compatibility_native_test.go"}, false, want("marketplace_e2e", "harness_compatibility")},
		{"harness script", []string{"tools/harness-compat/install.sh"}, false, want("harness_compatibility")},
		{"TUI QA", []string{"tools/tui-qa/package.json"}, false, want("tui_qa")},
		{"marketplace Dockerfile", []string{"Dockerfile.marketplace"}, false, want("container_config", "container_marketplace")},
		{"root dockerignore", []string{".dockerignore"}, false, want("container_config", "container_marketplace")},
		{"web", []string{"web/app/main.ts"}, false, want("container_web")},
		{"site", []string{"site/index.md"}, false, want("container_site")},
		{"design", []string{"design/colors.css"}, false, want("container_config", "container_web", "container_site")},
		{"Helm", []string{"deploy/helm/agent-marketplace/values.yaml"}, false, want("container_config", "helm")},
		{"backend non-Go under benchmark root", []string{"internal/resolver/fixture.json"}, false, want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace")},
		{"backend non-Go not under benchmark root", []string{"cmd/marketplace-healthcheck/config.yaml"}, false, want("go_build", "go_test", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace")},
		{"backend prose remains no-op", []string{"internal/tui/README.md"}, false, want()},
		{"unknown script", []string{"scripts/new-check.sh"}, false, all},
		{"unknown config", []string{"config/policy.yaml"}, false, all},
		{"LICENSE", []string{"LICENSE"}, false, want()},
		{"NOTICE", []string{"NOTICE"}, false, want()},
		{"web LICENSE", []string{"web/LICENSE"}, false, want()},
		{"site NOTICE", []string{"site/NOTICE"}, false, want()},
		{"backend nested LICENSE", []string{"internal/resolver/LICENSE"}, false, want()},
		{"docs LICENSE", []string{"docs/LICENSE"}, false, want()},
		{"design LICENSE", []string{"design/LICENSE"}, false, want()},
		{"ci workflow", []string{".github/workflows/ci.yml"}, false, all},
		{"harness-compatibility workflow", []string{".github/workflows/harness-compatibility.yml"}, false, all},
		{"ci-plan source", []string{"tools/ci-plan/main.go"}, false, all},
		{"Makefile", []string{"Makefile"}, false, all},
		{"go.mod", []string{"go.mod"}, false, want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace")},
		{"go.sum", []string{"go.sum"}, false, want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace")},
		{"go.work", []string{"go.work"}, false, want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace")},
		{"go.work.sum", []string{"go.work.sum"}, false, want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace")},
		{"e2e go.mod", []string{"e2e/go.mod"}, false, want("harness_compatibility", "marketplace_e2e")},
		{"e2e go.sum", []string{"e2e/go.sum"}, false, want("harness_compatibility", "marketplace_e2e")},
		{"multi-path union", []string{"internal/resolver/source.go", "web/index.html", "deploy/helm/agent-marketplace/Chart.yaml"}, false, want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_config", "container_marketplace", "container_web", "helm")},
	}

	for _, root := range expectedBenchmarkRoots {
		p := path.Join(root, "file.go")
		cases = append(cases, struct {
			name  string
			paths []string
			all   bool
			want  map[string]bool
		}{
			name:  "benchmark root " + root,
			paths: []string{p},
			all:   false,
			want:  want("go_build", "go_test", "benchmark", "harness_compatibility", "marketplace_e2e", "tui_qa", "container_marketplace"),
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := planCategories(tc.paths, tc.all)
			if err != nil {
				t.Fatalf("planCategories: %v", err)
			}
			assertCategories(t, got, tc.want)
		})
	}
}

func TestDefinitionsMatch(t *testing.T) {
	if !slices.Equal(categories, expectedCategories) {
		t.Errorf("categories mismatch\n got: %v\nwant: %v", categories, expectedCategories)
	}
	if !slices.Equal(benchmarkRoots, expectedBenchmarkRoots) {
		t.Errorf("benchmarkRoots mismatch\n got: %v\nwant: %v", benchmarkRoots, expectedBenchmarkRoots)
	}
}

func TestHelmDoesNotEnableImageCategories(t *testing.T) {
	got, err := planCategories([]string{"deploy/helm/agent-marketplace/values.yaml"}, false)
	if err != nil {
		t.Fatalf("planCategories: %v", err)
	}
	for _, cat := range []string{"container_marketplace", "container_web", "container_site"} {
		if got[cat] {
			t.Errorf("%s should not be enabled for Helm paths", cat)
		}
	}
	for _, cat := range []string{"container_config", "helm"} {
		if !got[cat] {
			t.Errorf("%s should be enabled for Helm paths", cat)
		}
	}
}

func TestParseInputRejectsMalformed(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"absolute", "/etc/passwd\x00"},
		{"dot", "./foo\x00"},
		{"double-dot", "../foo\x00"},
		{"backslash", "foo\\bar\x00"},
		{"unclean", "foo//bar\x00"},
		{"empty-record", "foo\x00\x00bar\x00"},
		{"trailing-dot", "foo/\x00"},
		{"unterminated", "foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseInput([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected rejection for %q", tc.input)
			}
		})
	}
}

func TestMainOutput(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run --all: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(expectedCategories) {
		t.Fatalf("expected %d output lines, got %d:\n%s", len(expectedCategories), len(lines), out)
	}
	for i, line := range lines {
		want := expectedCategories[i] + "=true"
		if line != want {
			t.Errorf("line %d = %q, want %q", i, line, want)
		}
	}
}

func TestMainOutputStdin(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = strings.NewReader("tools/ci-plan/main.go\x00")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run with stdin: %v\n%s", err, out)
	}
	if !slices.Contains(strings.Split(strings.TrimSpace(string(out)), "\n"), "go_test=true") {
		t.Fatalf("expected go_test=true in output:\n%s", out)
	}
}

func TestMainExtraArgument(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--all", "extra")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error for --all extra, got:\n%s", out)
	}
	got := string(out)
	if !strings.Contains(got, `unexpected positional argument "extra"`) {
		t.Fatalf("expected error to blame extra, got:\n%s", got)
	}
	if strings.Contains(got, `unexpected positional argument "--all"`) {
		t.Fatalf("error incorrectly blames --all:\n%s", got)
	}
}
