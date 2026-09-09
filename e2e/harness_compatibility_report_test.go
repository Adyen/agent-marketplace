package e2e_test

import (
	"encoding/json"
	"os"
)

type compatibilityResult struct {
	Harness    string                   `json:"harness"`
	Resource   string                   `json:"resource"`
	Scope      string                   `json:"scope"`
	Mode       string                   `json:"materialization_mode"`
	Status     string                   `json:"status"`
	Version    string                   `json:"observed_version,omitempty"`
	Reason     string                   `json:"reason"`
	Commands   []compatibilityCommand   `json:"commands,omitempty"`
	Files      map[string]string        `json:"generated_fixture_files,omitempty"`
	Connection *compatibilityConnection `json:"connection,omitempty"`
}

type compatibilityConnection struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type compatibilityReport struct {
	Target          string                `json:"target"`
	NativeMode      string                `json:"native_mode"`
	ExpectedVersion string                `json:"expected_version,omitempty"`
	Results         []compatibilityResult `json:"results"`
}

func (r compatibilityReport) failed() bool {
	for _, result := range r.Results {
		if result.Status == "failed" || result.Status == "blocked" || result.Connection != nil && (result.Connection.Status == "failed" || result.Connection.Status == "blocked") {
			return true
		}
	}
	return false
}

func writeCompatibilityReport(path string, report compatibilityReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
