package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type summaryEvidence struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type summaryResult struct {
	Harness    string           `json:"harness"`
	Resource   string           `json:"resource"`
	Scope      string           `json:"scope"`
	Mode       string           `json:"materialization_mode"`
	Status     string           `json:"status"`
	Version    string           `json:"observed_version"`
	Reason     string           `json:"reason"`
	Trust      string           `json:"fixture_trust"`
	Connection *summaryEvidence `json:"connection"`
}

type summaryReport struct {
	Target          string          `json:"target"`
	NativeMode      string          `json:"native_mode"`
	ExpectedVersion string          `json:"expected_version"`
	Results         []summaryResult `json:"results"`
}

func readSummaryReport(path string) (summaryReport, error) {
	var report summaryReport
	file, err := os.Open(path)
	if err != nil {
		return report, fmt.Errorf("compatibility report cannot be read")
	}
	defer func() { _ = file.Close() }()
	const limit = 32 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(data) > limit || json.Unmarshal(data, &report) != nil {
		return report, fmt.Errorf("compatibility report is malformed or oversized")
	}
	if report.Target != "all" && !target.IsValid(report.Target) || len(report.Results) == 0 || !slices.Contains([]string{"probe", "unavailable", "blocked"}, report.NativeMode) {
		return report, fmt.Errorf("compatibility report lacks a valid target, native mode, or evidence rows")
	}
	statuses := []string{"native-verified", "config-only-verified", "unsupported-as-expected", "blocked", "failed"}
	for _, row := range report.Results {
		if !target.IsValid(row.Harness) || report.Target != "all" && row.Harness != report.Target || row.Resource == "" || row.Reason == "" || !slices.Contains([]string{"repo", "user"}, row.Scope) || !slices.Contains([]string{"symlink", "file"}, row.Mode) || !slices.Contains(statuses, row.Status) || row.Status == "native-verified" && row.Version == "" {
			return report, fmt.Errorf("compatibility report contains incomplete or invalid evidence")
		}
		if row.Trust != "" && (row.Trust != "repository-only" || !slices.Contains([]string{"claude", "codex", "gemini", "cursor"}, row.Harness)) {
			return report, fmt.Errorf("compatibility report contains invalid fixture trust evidence")
		}
		if row.Connection != nil && (row.Connection.Reason == "" || !slices.Contains([]string{"native-verified", "blocked", "failed", "not-probed"}, row.Connection.Status)) {
			return report, fmt.Errorf("compatibility report contains invalid connection evidence")
		}
	}
	return report, nil
}

func markdownCell(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = html.EscapeString(value)
	return strings.NewReplacer("\\", "\\\\", "|", "\\|", "`", "\\`", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "*", "\\*", "_", "\\_").Replace(value)
}

func summarize(out io.Writer, reportPath, image, created, source string, now time.Time) error {
	var text strings.Builder
	text.WriteString("## Harness compatibility\n\n")
	fail := func(err error) error {
		fmt.Fprintf(&text, "**Report unavailable or invalid:** %s\n", markdownCell(err.Error()))
		if _, writeErr := io.WriteString(out, text.String()); writeErr != nil {
			return writeErr
		}
		return err
	}
	if !slices.Contains([]string{"published", "bootstrap", "config-only"}, source) {
		return fail(fmt.Errorf("source must be published, bootstrap, or config-only"))
	}
	fmt.Fprintf(&text, "Image source: %s\n\n", markdownCell(source))
	if source != "config-only" {
		stamp, err := time.Parse(time.RFC3339, created)
		if err != nil || image == "" || stamp.After(now.Add(5*time.Minute)) {
			return fail(fmt.Errorf("native image provenance requires an image and valid RFC3339 creation time"))
		}
		age := max(time.Duration(0), now.Sub(stamp))
		fmt.Fprintf(&text, "Image: %s\n\nCreated: %s; age: %dh\n\n", markdownCell(image), markdownCell(stamp.UTC().Format(time.RFC3339)), int64(age/time.Hour))
		if age >= 48*time.Hour {
			text.WriteString("**STALE: image is at least 48 hours old.** Daily builds should select newest stable immediately; there is no release-age buffer.\n\n")
		}
	} else {
		text.WriteString("No native image execution is claimed by this config-only job.\n\n")
	}
	report, err := readSummaryReport(reportPath)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(&text, "Target: %s; native mode: %s; expected version: %s\n\n", markdownCell(report.Target), markdownCell(report.NativeMode), markdownCell(report.ExpectedVersion))
	text.WriteString("CLI setup only is not resource acceptance. Resource evidence and MCP transport evidence are reported independently.\n\n")
	text.WriteString("| Harness | Evidence | Resource | Scope | Mode | Result | Actual version | Connection | Reason / capability gap |\n|---|---|---|---|---|---|---|---|---|\n")
	failed := false
	for _, row := range report.Results {
		kind := "Resource evidence"
		if row.Resource == "native-cli" {
			kind = "CLI setup only"
		}
		version := row.Version
		if version == "" {
			version = "not observed"
		}
		connection, reason := "not probed", row.Reason
		if row.Trust != "" {
			reason += "; untrusted gate verified before repository-only fixture trust was granted"
		}
		if row.Connection != nil {
			connection = row.Connection.Status
			reason += "; connection: " + row.Connection.Reason
			failed = failed || row.Connection.Status == "failed" || row.Connection.Status == "blocked"
		}
		failed = failed || row.Status == "failed" || row.Status == "blocked"
		fields := []string{row.Harness, kind, row.Resource, row.Scope, row.Mode, row.Status, version, connection, reason}
		for i := range fields {
			fields[i] = markdownCell(fields[i])
		}
		fmt.Fprintf(&text, "| %s |\n", strings.Join(fields, " | "))
	}
	if _, err := io.WriteString(out, text.String()); err != nil {
		return err
	}
	if failed {
		return fmt.Errorf("report contains failed or blocked evidence")
	}
	return nil
}
