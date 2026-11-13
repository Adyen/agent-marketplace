package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

type doctorJSONStep struct {
	ID        *int            `json:"id"`
	Code      *string         `json:"code"`
	Class     *string         `json:"class"`
	Action    *string         `json:"action"`
	Files     *[]string       `json:"files"`
	Status    *string         `json:"status"`
	BlockedBy json.RawMessage `json:"blocked_by"`
}

type doctorJSONResult struct {
	ID     *int    `json:"id"`
	Status *string `json:"status"`
}

type doctorJSONReport struct {
	Steps   []doctorJSONStep   `json:"steps"`
	Results []doctorJSONResult `json:"results"`
}

func decodeDoctorJSON(t *testing.T, output string) doctorJSONReport {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var report doctorJSONReport
	if err := decoder.Decode(&report); err != nil {
		t.Fatalf("decode doctor JSON report: %v\n%s", err, output)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("doctor JSON has trailing document or text: %v", err)
	}
	if report.Steps == nil || report.Results == nil {
		t.Fatalf("doctor report arrays must be non-null: %s", output)
	}
	for _, step := range report.Steps {
		if step.ID == nil || step.Code == nil || step.Class == nil || step.Action == nil ||
			step.Files == nil || step.Status == nil || step.BlockedBy == nil {
			t.Fatalf("step omits a required field: %s", output)
		}
	}
	for _, result := range report.Results {
		if result.ID == nil || result.Status == nil {
			t.Fatalf("result omits a required field: %s", output)
		}
	}
	return report
}

// spec: doctor-repair-planning/json-repair-reports-a-structured-plan-and-results
func TestDoctorRepairJSONCommandOutputsOneCleanDocument(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	devinDoctorEnv(t)
	output, err := executeCommand("doctor", "--repair", "--json")
	if err != nil {
		t.Fatalf("doctor --repair --json: %v\n%s", err, output)
	}
	report := decodeDoctorJSON(t, output)
	if len(report.Steps) != 0 || len(report.Results) != 0 {
		t.Fatalf("clean repair report = %+v, want empty arrays", report)
	}
}

type doctorJSONPanicReader struct{}

func (doctorJSONPanicReader) Read([]byte) (int, error) {
	panic("JSON repair read stdin")
}

// spec: doctor-repair-planning/json-repair-without-yes-never-writes-or-prompts
func TestDoctorRepairJSONPreviewDoesNotReadTerminalInputOrWrite(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	assertUnchanged := snapshotDoctorRepairFiles(t, repoRoot, mcpPath)
	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:     &out,
		in:      doctorJSONPanicReader{},
		isTTY:   true,
		repair:  true,
		json:    true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err != nil {
		t.Fatalf("JSON preview: %v\n%s", err, out.String())
	}
	report := decodeDoctorJSON(t, out.String())
	if len(report.Steps) != 1 || *report.Steps[0].Status != "pending" ||
		*report.Steps[0].Class != "safe" || string(report.Steps[0].BlockedBy) != "null" {
		t.Fatalf("JSON preview step = %+v", report.Steps)
	}
	if len(report.Results) != 0 {
		t.Fatalf("preview reported execution results: %+v", report.Results)
	}
	assertUnchanged()
}

// spec: doctor-repair-planning/json-repair-with-yes-runs-only-safe-steps
// spec: doctor-repair-planning/json-repair-never-prints-mcp-secrets
func TestDoctorRepairJSONYesAdoptsAndDefersConflictingMCP(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	const envSecret = "json-env-secret-sentinel"
	const headerSecret = "json-header-secret-sentinel"
	selection := []resolver.ResolvedArtifact{devinMcpArtifact("managed", "managed-binary")}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed exact MCP aggregate: %v", err)
	}
	mcpPath := filepath.Join(repoRoot, ".devin", harness.DevinMcpConfigFile)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	conflicting := `{"command":"handwritten-binary","env":{"API_TOKEN":"` + envSecret +
		`"},"headers":{"Authorization":"` + headerSecret + `"}}`
	withConflict := strings.Replace(string(before), `"mcpServers": {`,
		`"mcpServers": {"conflicting": `+conflicting+`,`, 1)
	if withConflict == string(before) {
		t.Fatalf("fixture did not add a destructive MCP conflict:\n%s", before)
	}
	if err := os.WriteFile(mcpPath, []byte(withConflict), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	selection = append(selection, devinMcpArtifact("conflicting", "generated-binary"))
	before, err = os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(before, []byte(envSecret)) || !bytes.Contains(before, []byte(headerSecret)) {
		t.Fatalf("MCP fixture does not contain both secret sentinels:\n%s", before)
	}

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		repair:  true,
		yes:     true,
		json:    true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err != nil {
		t.Fatalf("JSON repair with --yes: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), envSecret) || strings.Contains(out.String(), headerSecret) {
		t.Fatalf("JSON repair disclosed an MCP value:\n%s", out.String())
	}
	report := decodeDoctorJSON(t, out.String())
	var completed, deferred bool
	for _, step := range report.Steps {
		switch {
		case *step.Class == "safe" && *step.Status == "completed":
			completed = true
		case *step.Class == "destructive" && *step.Status == "deferred":
			deferred = true
		}
	}
	if !completed || !deferred {
		t.Fatalf("safe/destructive JSON statuses = %+v", report.Steps)
	}
	if len(report.Results) != 1 || *report.Results[0].Status != "completed" {
		t.Fatalf("JSON execution results = %+v", report.Results)
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("safe adoption changed MCP aggregate bytes: %v\n%s", err, after)
	}
	if _, err := os.Stat(store.StateFile()); err != nil {
		t.Fatalf("safe adoption did not record ownership: %v", err)
	}
}

// spec: doctor-repair-planning/json-repair-never-prints-mcp-secrets
func TestDoctorRepairJSONRedactsDiagnosisError(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	devinDoctorEnv(t)
	const secret = "diagnosis-error-secret-sentinel"
	diagnosisErr := errors.New("resolution failed with " + secret)
	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:    &out,
		repair: true,
		json:   true,
		resolve: func(context.Context, string) ([]resolver.ResolvedArtifact, error) {
			return nil, diagnosisErr
		},
	})
	if !errors.Is(err, diagnosisErr) {
		t.Fatalf("JSON repair error = %v, want wrapped diagnosis error", err)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("JSON repair disclosed a diagnosis error:\n%s", out.String())
	}
	report := decodeDoctorJSON(t, out.String())
	if len(report.Steps) == 0 || *report.Steps[0].Class != "human-only" {
		t.Fatalf("blocked diagnosis report = %+v", report.Steps)
	}
	for _, step := range report.Steps {
		if strings.Contains(*step.Action, secret) {
			t.Fatalf("diagnosis error leaked in action: %+v", step)
		}
	}
}

// spec: doctor-repair-planning/json-repair-preserves-text-repair-exit-status
func TestDoctorRepairJSONExitStatusMatchesText(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	blockedCause := errors.New("blocked resolution")
	run := func(t *testing.T, mode, format string) error {
		t.Helper()
		repoRoot, _ := devinDoctorEnv(t)
		opts := doctorOptions{
			repair: true,
			json:   format == "json",
			out:    io.Discard,
			resolve: func(context.Context, string) ([]resolver.ResolvedArtifact, error) {
				return nil, blockedCause
			},
		}
		switch mode {
		case "clean":
			opts.resolve = staticResolve(nil)
		case "preview":
			_, selection := prepareDevinRepairFixture(t, repoRoot)
			opts.resolve = staticResolve(selection)
		case "safe execution":
			_, selection := prepareDevinRepairFixture(t, repoRoot)
			opts.resolve = staticResolve(selection)
			opts.yes = true
		}
		return runDoctor(opts)
	}
	for _, mode := range []string{"clean", "preview", "blocked", "safe execution"} {
		t.Run(mode, func(t *testing.T) {
			var textErr, jsonErr error
			t.Run("text", func(t *testing.T) { textErr = run(t, mode, "text") })
			t.Run("json", func(t *testing.T) { jsonErr = run(t, mode, "json") })
			if (textErr == nil) != (jsonErr == nil) {
				t.Fatalf("text error = %v, JSON error = %v", textErr, jsonErr)
			}
			if errors.Is(textErr, blockedCause) != errors.Is(jsonErr, blockedCause) {
				t.Fatalf("text errors.Is(blockedCause) = %t, JSON = %t",
					errors.Is(textErr, blockedCause), errors.Is(jsonErr, blockedCause))
			}
			if mode == "blocked" && !errors.Is(jsonErr, blockedCause) {
				t.Fatalf("JSON blocked error lost cause: %v", jsonErr)
			}
		})
	}
}

// spec: doctor-repair-planning/json-without-repair-is-rejected
func TestDoctorJSONWithoutRepairIsRejected(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	devinDoctorEnv(t)
	output, err := executeCommand("doctor", "--json")
	if err == nil || !strings.Contains(output, "--json only applies to --repair") {
		t.Fatalf("doctor --json error = %v, output = %s", err, output)
	}
}
