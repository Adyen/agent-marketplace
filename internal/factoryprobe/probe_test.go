package factoryprobe

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const (
	enableEnv       = "AGENT_MARKETPLACE_FACTORY_PROBES"
	expectedVersion = "0.199.0"
)

var expectedToolNames = []string{
	"ConnectorSearch",
	"Create",
	"DismissHandoffItems",
	"Edit",
	"EndFeatureRun",
	"Execute",
	"ExitSpecMode",
	"FetchUrl",
	"GenerateDroid",
	"GenerateImage",
	"Glob",
	"Grep",
	"LS",
	"Loop",
	"ProposeMission",
	"Read",
	"Skill",
	"StartMissionRun",
	"Task",
	"TaskOutput",
	"TaskStop",
	"TodoWrite",
	"ToolSearch",
	"WebSearch",
}

var expectedDefaultTools = []string{
	"ConnectorSearch",
	"Execute",
	"FetchUrl",
	"Glob",
	"Grep",
	"LS",
	"Read",
	"Skill",
	"TodoWrite",
	"ToolSearch",
	"WebSearch",
}

func TestToolVocabulary(t *testing.T) {
	path := requireDroid(t)
	out, err := runDroid(t, path, "exec", "--list-tools")
	if err != nil {
		t.Fatalf("list default tools: %v\n%s", err, out)
	}
	statuses := parseToolStatuses(t, out)
	if got := sortedNames(statuses, ""); !slices.Equal(got, expectedToolNames) {
		t.Fatalf("tool vocabulary = %v, want %v", got, expectedToolNames)
	}
}

func TestUnknownToolIsRejected(t *testing.T) {
	path := requireDroid(t)
	out, err := runDroid(t, path, "exec", "--enabled-tools", "UnknownTool", "--list-tools")
	if err == nil {
		t.Fatalf("unknown tool accepted:\n%s", out)
	}
	if !strings.Contains(out, "Unknown tool identifier(s)") {
		t.Fatalf("unknown tool output = %q", out)
	}
}

func TestMissingToolsUsesDefaultSet(t *testing.T) {
	path := requireDroid(t)
	out, err := runDroid(t, path, "exec", "--list-tools")
	if err != nil {
		t.Fatalf("list default tools: %v\n%s", err, out)
	}
	if got := sortedNames(parseToolStatuses(t, out), "allowed"); !slices.Equal(got, expectedDefaultTools) {
		t.Fatalf("default tools = %v, want %v", got, expectedDefaultTools)
	}
}

func TestExplicitToolsRestrictDefaultSet(t *testing.T) {
	path := requireDroid(t)
	out, err := runDroid(t, path, "exec", "--enabled-tools", "Read", "--list-tools")
	if err != nil {
		t.Fatalf("list restricted tools: %v\n%s", err, out)
	}
	if got := sortedNames(parseToolStatuses(t, out), "allowed"); !slices.Equal(got, []string{"Read"}) {
		t.Fatalf("explicitly enabled tools = %v, want [Read]", got)
	}
}

func requireDroid(t *testing.T) string {
	t.Helper()
	if os.Getenv(enableEnv) != "1" {
		t.Skipf("set %s=1 to run Factory runtime probes", enableEnv)
	}
	path, err := exec.LookPath("droid")
	if err != nil {
		t.Skip("droid is not installed")
	}
	out, err := runDroid(t, path, "--version")
	if err != nil {
		t.Fatalf("droid --version: %v\n%s", err, out)
	}
	version := strings.TrimSpace(out)
	if version != expectedVersion {
		t.Fatalf("Factory probe baseline requires droid %s; installed %s must be re-verified before updating the baseline", expectedVersion, version)
	}
	t.Logf("Factory runtime: droid %s", version)
	return path
}

func runDroid(t *testing.T, path string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(path, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"PATH=" + os.Getenv("PATH"),
		"TERM=dumb",
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func parseToolStatuses(t *testing.T, output string) map[string]string {
	t.Helper()
	statuses := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "• ") {
			continue
		}
		tool, status, ok := strings.Cut(strings.TrimPrefix(line, "• "), " - status: ")
		if !ok {
			t.Fatalf("unrecognized tool line %q", line)
		}
		if index := strings.Index(tool, " ("); index >= 0 {
			tool = tool[:index]
		}
		statuses[tool] = status
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(statuses) == 0 {
		t.Fatalf("no tool statuses in output:\n%s", output)
	}
	return statuses
}

func sortedNames(statuses map[string]string, status string) []string {
	names := make([]string, 0, len(statuses))
	for name, gotStatus := range statuses {
		if status == "" || gotStatus == status {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
