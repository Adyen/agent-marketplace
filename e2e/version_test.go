package e2e_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestReleaseVersionMatchesTelemetryPreview(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	env.WriteGlobalConfig("harnesses: [factory]\n")

	result := env.RunAM("--version")
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "e2e-test" {
		t.Fatalf("expected linker-stamped version e2e-test, got exit %d: %s", result.ExitCode, result.Combined)
	}
	result = env.RunAM("telemetry", "preview", "--level", "detailed")
	if result.ExitCode != 0 {
		t.Fatalf("preview failed: %s", result.Combined)
	}
	line, _, _ := strings.Cut(result.Stdout, "\n")
	path, ok := strings.CutPrefix(line, "Preview payload written to ")
	if !ok {
		t.Fatalf("missing preview path: %s", result.Stdout)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ResourceAttributes map[string]string `json:"resource_attributes"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload.ResourceAttributes["cli.version"]; got != "e2e-test" {
		t.Fatalf("preview cli.version = %q, want linker-stamped version e2e-test", got)
	}
}
