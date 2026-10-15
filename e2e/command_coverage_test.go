package e2e_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The built binary records every E2E invocation (command, flags set, the
// apiVersion of each configuration layer at start, and success). After a
// complete run, every public command and flag must have succeeded at least
// once with every present consumer layer declaring exact v4. First-run
// commands may create absent roots. Schema-independent commands need one
// successful behavioral invocation without a consumer configuration dependency.
//
// Known gaps live in commandCoverageGapsFile. The guard fails when an
// uncovered requirement is not listed there, and when a listed gap is covered
// or no longer names a public command or flag, so the list can only shrink.

const commandCoverageGapsFile = "testdata/command-coverage-gaps.txt"

// e2eInvocationDir collects the binary's invocation records; TestMain creates
// it and subprocessEnv passes it to every invocation.
var e2eInvocationDir string

// schemaIndependentCommands never read the layered client configuration.
var schemaIndependentCommands = map[string]string{
	"completion":            "prints a generated shell script",
	"completion bash":       "prints a generated shell script",
	"completion fish":       "prints a generated shell script",
	"completion powershell": "prints a generated shell script",
	"completion zsh":        "prints a generated shell script",
	"help":                  "prints command help",
	"serve":                 "runs the Marketplace server from its own --config",
	"serve adopt":           "maintains the Marketplace server from its own --config",
	"serve reindex":         "maintains the Marketplace server from its own --config",
	"serve reset":           "maintains the Marketplace server from its own --config",
}

type commandTree struct {
	Commands []string `json:"commands"`
	Flags    []string `json:"flags"`
}

type commandInvocation struct {
	Command string            `json:"command"`
	Flags   []string          `json:"flags"`
	OK      bool              `json:"ok"`
	Schemas map[string]string `json:"schemas"`
}

// schemaClass accepts only complete evidence about all four consumer roots.
// Absent roots are legitimate first-run inputs, not unreadable/versionless files.
func schemaClass(schemas map[string]string) string {
	for _, layer := range []string{"org", "user", "repo", "local"} {
		switch schemas[layer] {
		case "absent", "v4":
		default:
			return "unsupported"
		}
	}
	return "v4"
}

// coverageRequirements names each requirement as "<command or flag> [class]".
func coverageRequirements(tree commandTree) []string {
	var reqs []string
	add := func(name string, independent bool) {
		if independent {
			reqs = append(reqs, name+" [any]")
			return
		}
		reqs = append(reqs, name+" [v4]")
	}
	for _, command := range tree.Commands {
		_, independent := schemaIndependentCommands[command]
		add(command, independent)
	}
	for _, flag := range tree.Flags {
		command, _, _ := strings.Cut(flag, " --")
		_, independent := schemaIndependentCommands[command]
		add(flag, independent)
	}
	slices.Sort(reqs)
	return reqs
}

func coveredRequirements(invocations []commandInvocation) map[string]bool {
	covered := map[string]bool{}
	for _, invocation := range invocations {
		if !invocation.OK {
			continue
		}
		class := schemaClass(invocation.Schemas)
		for _, name := range append([]string{invocation.Command}, invocation.Flags...) {
			covered[name+" [any]"] = true
			covered[name+" ["+class+"]"] = true
		}
	}
	return covered
}

// commandCoverageFindings returns uncovered requirements missing from gaps,
// and listed gaps that are covered or no longer required.
func commandCoverageFindings(tree commandTree, invocations []commandInvocation, gaps []string) (uncovered, stale []string) {
	reqs := coverageRequirements(tree)
	covered := coveredRequirements(invocations)
	for _, req := range reqs {
		if !covered[req] && !slices.Contains(gaps, req) {
			uncovered = append(uncovered, req)
		}
	}
	for _, gap := range gaps {
		if covered[gap] || !slices.Contains(reqs, gap) {
			stale = append(stale, gap)
		}
	}
	return uncovered, stale
}

func readCommandCoverageGaps(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var gaps []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" && !strings.HasPrefix(line, "#") {
			gaps = append(gaps, line)
		}
	}
	return gaps, scanner.Err()
}

func readCommandInvocations(dir string) ([]commandInvocation, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "invocation-*.json"))
	if err != nil {
		return nil, err
	}
	invocations := make([]commandInvocation, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var invocation commandInvocation
		if err := json.Unmarshal(data, &invocation); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		invocations = append(invocations, invocation)
	}
	return invocations, nil
}

func readCommandTree(binary string) (commandTree, error) {
	var tree commandTree
	dir, err := os.MkdirTemp("", "agent-marketplace-command-tree-")
	if err != nil {
		return tree, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "tree.json")
	cmd := exec.Command(binary)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "AGENT_MARKETPLACE_E2E_COMMAND_TREE=" + path}
	if output, err := cmd.CombinedOutput(); err != nil {
		return tree, fmt.Errorf("read command tree: %w\n%s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tree, err
	}
	if err := json.Unmarshal(data, &tree); err != nil {
		return tree, err
	}
	if len(tree.Commands) == 0 {
		return tree, errors.New("binary reported no public commands")
	}
	return tree, nil
}

// completeRun reports whether this run executed the whole suite; filtered
// runs cannot judge coverage.
func completeRun() bool {
	for _, name := range []string{"test.run", "test.skip", "test.list"} {
		if f := flag.Lookup(name); f != nil && f.Value.String() != "" {
			return false
		}
	}
	return !testing.Short()
}

// checkCommandCoverage runs after a passing, unfiltered suite.
func checkCommandCoverage(binary string) error {
	if !completeRun() {
		fmt.Println("e2e: command coverage not evaluated for a filtered run")
		return nil
	}
	tree, err := readCommandTree(binary)
	if err != nil {
		return err
	}
	invocations, err := readCommandInvocations(e2eInvocationDir)
	if err != nil {
		return err
	}
	gaps, err := readCommandCoverageGaps(commandCoverageGapsFile)
	if err != nil {
		return err
	}
	uncovered, stale := commandCoverageFindings(tree, invocations, gaps)
	reqs, covered := coverageRequirements(tree), coveredRequirements(invocations)
	met := 0
	for _, req := range reqs {
		if covered[req] {
			met++
		}
	}
	fmt.Printf("e2e: command coverage: %d invocations, %d of %d requirements covered, %d listed gaps\n", len(invocations), met, len(reqs), len(gaps))
	var problems []string
	for _, req := range uncovered {
		problems = append(problems, fmt.Sprintf("  %s: no successful E2E invocation; add an E2E test that runs it with that configuration schema", req))
	}
	for _, gap := range stale {
		problems = append(problems, fmt.Sprintf("  %s: listed in %s but covered or no longer public; remove it", gap, commandCoverageGapsFile))
	}
	if len(problems) > 0 {
		return fmt.Errorf("command coverage guard failed:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}

// spec: regression-gates/consumer-command-coverage-requires-v4-success
func TestCommandCoverageGuardRejectsNewAndStaleGaps(t *testing.T) {
	tree := commandTree{Commands: []string{"sync"}, Flags: []string{"root --debug", "sync --check"}}
	v4 := map[string]string{"org": "v4", "user": "v4", "repo": "v4", "local": "v4"}
	for _, layer := range []string{"org", "user", "repo", "local"} {
		for _, version := range []string{"v1", "v2", "v3", "v99", "unversioned", "unreadable", ""} {
			t.Run(layer+"/"+version, func(t *testing.T) {
				schemas := map[string]string{"org": "v4", "user": "v4", "repo": "v4", "local": "v4"}
				schemas[layer] = version
				bad := commandInvocation{Command: "sync", Flags: tree.Flags, OK: true, Schemas: schemas}
				if uncovered, _ := commandCoverageFindings(tree, []commandInvocation{bad}, nil); len(uncovered) != 3 {
					t.Fatalf("unsupported %s %q counted as success: %v", layer, version, uncovered)
				}
			})
		}
	}
	invocations := []commandInvocation{
		{Command: "sync", Flags: []string{"root --debug"}, OK: true, Schemas: v4},
		{Command: "sync", Flags: []string{"sync --check"}, OK: false, Schemas: v4},
	}
	uncovered, stale := commandCoverageFindings(tree, invocations, []string{"sync [v4]"})
	if !slices.Equal(uncovered, []string{"sync --check [v4]"}) || !slices.Equal(stale, []string{"sync [v4]"}) {
		t.Fatalf("uncovered=%v stale=%v; refusals must not cover operational flags", uncovered, stale)
	}
	absent := map[string]string{"org": "absent", "user": "absent", "repo": "absent", "local": "absent"}
	invocations = append(invocations, commandInvocation{Command: "sync", Flags: tree.Flags, OK: true, Schemas: absent})
	if uncovered, _ := commandCoverageFindings(tree, invocations, nil); len(uncovered) != 0 {
		t.Fatalf("first-run absence did not count: %v", uncovered)
	}
	tree.Commands = append(tree.Commands, "untested")
	if uncovered, _ := commandCoverageFindings(tree, invocations, nil); !slices.Equal(uncovered, []string{"untested [v4]"}) {
		t.Fatalf("missing invocation was not reported: %v", uncovered)
	}
	if _, stale := commandCoverageFindings(tree, invocations, []string{"removed [v4]"}); !slices.Contains(stale, "removed [v4]") {
		t.Fatalf("removed public surface gap not stale: %v", stale)
	}
}

// spec: regression-gates/schema-independent-surfaces-remain-successfully-exercised
func TestCommandCoverageSchemaIndependentSuccess(t *testing.T) {
	tree := commandTree{Commands: []string{"completion bash", "help", "serve"}, Flags: []string{"serve --config"}}
	invocations := []commandInvocation{
		{Command: "completion bash", OK: true},
		{Command: "help", OK: true},
		{Command: "serve", Flags: []string{"serve --config"}, OK: true},
	}
	if uncovered, _ := commandCoverageFindings(tree, invocations, nil); len(uncovered) != 0 {
		t.Fatalf("configuration-free successful surfaces not covered: %v", uncovered)
	}
	for i := range invocations {
		invocations[i].OK = false
	}
	if uncovered, _ := commandCoverageFindings(tree, invocations, nil); len(uncovered) != 4 {
		t.Fatalf("independent refusals counted: %v", uncovered)
	}
}

func TestCommandCoverageRecorderIgnoresHelpRequests(t *testing.T) {
	env := newEnv(t)
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	dir := t.TempDir()
	record := map[string]string{"AGENT_MARKETPLACE_E2E_INVOCATION_DIR": dir}
	for _, args := range [][]string{{"sync", "--help"}, {"gc", "-h"}} {
		env.AssertExitCode(env.RunAMWithEnv(record, args...), 0)
	}
	if invocations, err := readCommandInvocations(dir); err != nil || len(invocations) != 0 {
		t.Fatalf("help requests were recorded as coverage: %v %+v", err, invocations)
	}
	env.AssertExitCode(env.RunAMWithEnv(record, "status"), 0)
	invocations, err := readCommandInvocations(dir)
	if err != nil || len(invocations) != 1 || invocations[0].Command != "status" || !invocations[0].OK || schemaClass(invocations[0].Schemas) != "v4" {
		t.Fatalf("status was not recorded as one successful v4 invocation: %v %+v", err, invocations)
	}
}
