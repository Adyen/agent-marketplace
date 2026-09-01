package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	otelattribute "go.opentelemetry.io/otel/attribute"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
)

// PreviewPayload is a representative local rendering of what telemetry at a
// given level would contain. It is never transmitted.
type PreviewPayload struct {
	SchemaVersion      string              `json:"schema_version"`
	Level              string              `json:"level"`
	EndpointHostname   string              `json:"endpoint_hostname"`
	RetentionPolicyURL string              `json:"retention_policy_url,omitempty"`
	InstallationID     string              `json:"installation_id"`
	ResourceAttributes map[string]any      `json:"resource_attributes"`
	Headers            []PreviewHeader     `json:"headers,omitempty"`
	Metrics            []PreviewMetric     `json:"metrics,omitempty"`
	Spans              []PreviewSpan       `json:"spans,omitempty"`
	DiagnosticEvents   []PreviewDiagnostic `json:"diagnostic_events,omitempty"`
	SensitiveWarning   string              `json:"sensitive_data_warning,omitempty"`
}

// PreviewHeader describes a primary telemetry header's name and how its value
// is sourced without exposing the value itself.
type PreviewHeader struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Reference string `json:"reference"`
}

// PreviewMetric is a representative metric sample.
type PreviewMetric struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Unit        string            `json:"unit"`
	Attributes  map[string]string `json:"attributes"`
	Value       any               `json:"value"`
}

// PreviewSpan is a representative trace span sample.
type PreviewSpan struct {
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes"`
}

// PreviewDiagnostic is a representative diagnostic event sample.
type PreviewDiagnostic struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Preview renders a representative local payload for the requested telemetry
// level and writes it to a user-only file under the durable state root. The
// returned path contains the sample payload; the payload is never transmitted.
func Preview(ctx context.Context, level string, state TelemetryState) (string, error) {
	level = normalizePreviewLevel(level)

	installationID := state.InstallationIDPath
	id, err := ReadInstallationID(installationID)
	if err != nil {
		return "", fmt.Errorf("read installation id: %w", err)
	}
	if id == "" {
		id = "<generated when telemetry is active>"
	}

	res, err := buildResource(ctx, id, tmcfg.SchemaVersion)
	if err != nil {
		slog.Warn("failed to build telemetry resource for preview; using empty resource", "error", err)
	}

	p := PreviewPayload{
		SchemaVersion:      tmcfg.SchemaVersion,
		Level:              level,
		EndpointHostname:   state.PrimaryEndpointHostname,
		RetentionPolicyURL: state.RetentionPolicyURL,
		InstallationID:     id,
		ResourceAttributes: resourceAttributes(res),
		Headers:            previewHeaders(state.PrimaryHeaders),
	}

	switch level {
	case tmcfg.LevelBasic, tmcfg.LevelDetailed, tmcfg.LevelDiagnostic:
		p.Metrics = []PreviewMetric{
			{
				Name:        string(tmcfg.CommandCounterName),
				Description: "Command invocations",
				Unit:        "{invocation}",
				Attributes: map[string]string{
					string(tmcfg.ResourceAttrCommandName): "telemetry preview",
					string(tmcfg.ResourceAttrOutcomeCode): "success",
				},
				Value: 1,
			},
			{
				Name:        string(tmcfg.CommandDurationName),
				Description: "Command duration in milliseconds",
				Unit:        "ms",
				Attributes: map[string]string{
					string(tmcfg.ResourceAttrCommandName): "telemetry preview",
					string(tmcfg.ResourceAttrOutcomeCode): "success",
				},
				Value: map[string]any{"bucket": "100-250ms"},
			},
		}
	}

	switch level {
	case tmcfg.LevelDetailed, tmcfg.LevelDiagnostic:
		p.Spans = []PreviewSpan{
			{
				Name: "agent_marketplace.command",
				Attributes: map[string]string{
					string(tmcfg.ResourceAttrCommandName): "telemetry preview",
					"command.flags":                       "--level " + level,
				},
			},
			{
				Name: "agent_marketplace.tui.config",
				Attributes: map[string]string{
					"harnesses":      "factory,devin",
					"harness.scope":  "repo",
					"registry.count": "1",
					"resource.count": "2",
				},
			},
			{
				Name: "agent_marketplace.tui.config_registry",
				Attributes: map[string]string{
					"registry.location": "https://git.example.com/team/resources.git",
					"registry.ref":      "main",
					"config.scope":      "user",
				},
			},
			{
				Name: "agent_marketplace.tui.config_resources",
				Attributes: map[string]string{
					"resource.kind":     "skill",
					"resource.names":    `["code-review","smith"]`,
					"registry.location": "https://git.example.com/team/resources.git",
					"config.scope":      "user",
				},
			},
		}
	}

	if level == tmcfg.LevelDiagnostic {
		p.DiagnosticEvents = []PreviewDiagnostic{
			{Name: "diagnostic.log", Message: "sample diagnostic log message"},
			{Name: "diagnostic.error", Message: "sample controlled error code"},
		}
		p.SensitiveWarning = "Diagnostic payloads may contain URLs (with credentials removed), filesystem paths, and error messages. Review before sharing."
	}

	sanitizePreviewPayload(&p)

	previewRoot := filepath.Join(state.StateRoot(), "preview")
	if err := os.MkdirAll(previewRoot, 0o700); err != nil {
		return "", fmt.Errorf("create preview directory: %w", err)
	}

	name := fmt.Sprintf("payload-%d-%s.json", time.Now().UTC().Unix(), randomPreviewSuffix())
	path := filepath.Join(previewRoot, name)

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal preview payload: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write preview payload: %w", err)
	}

	return path, nil
}

func normalizePreviewLevel(level string) string {
	switch level {
	case tmcfg.LevelBasic, tmcfg.LevelDetailed, tmcfg.LevelDiagnostic:
		return level
	default:
		return tmcfg.LevelBasic
	}
}

func previewHeaders(headers map[string]tmcfg.HeaderSource) []PreviewHeader {
	if len(headers) == 0 {
		return nil
	}
	out := make([]PreviewHeader, 0, len(headers))
	for name, src := range headers {
		kind, ref := src.Source()
		out = append(out, PreviewHeader{Name: name, Source: kind, Reference: ref})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func resourceAttributes(res *otelresource.Resource) map[string]any {
	attrs := make(map[string]any)
	if res == nil {
		return attrs
	}
	iter := res.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	return attrs
}

// randomPreviewSuffix returns a short random hex suffix for preview filenames.
func randomPreviewSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// AsInterface is declared on otelattribute.Value, but keep the import used.
var _ = otelattribute.String
