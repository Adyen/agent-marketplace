package windsurf

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// windsurfEnvPattern matches a standalone Windsurf ${env:VAR} interpolation.
var windsurfEnvPattern = regexp.MustCompile(`^\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}$`)

// windsurfEmbeddedEnvPattern matches embedded ${env:VAR} occurrences within a larger string.
var windsurfEmbeddedEnvPattern = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// LoadMcpServers reads a Windsurf mcp_config.json file and normalizes
// Windsurf-specific interpolation syntax into canonical (Claude-compatible)
// form so the IR is harness-neutral. Normalization applies to all string
// values in command, args, URL, headers, and recursively preserved extras.
func LoadMcpServers(src fs.FS, file string) ([]*ir.McpServer, []diag.Diagnostic, error) {
	servers, err := bundle.LoadMcpServers(src, file)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/windsurf: load mcp servers: %w", err)
	}
	var diags []diag.Diagnostic
	for _, server := range servers {
		server.Command = normalizeWindsurfMcpString(server.Command)
		for i, arg := range server.Args {
			server.Args[i] = normalizeWindsurfMcpString(arg)
		}
		server.URL = normalizeWindsurfMcpString(server.URL)

		if server.Env != nil {
			normalizedEnv := make(map[string]string, len(server.Env))
			for key, value := range server.Env {
				normalized := normalizeWindsurfMcpString(value)
				normalizedEnv[key] = normalized
				appendWindsurfMcpDiagnostics(&diags, server.Name(), file, key, value, normalized)
			}
			server.Env = normalizedEnv
		}
		if server.Headers != nil {
			normalizedHeaders := make(map[string]string, len(server.Headers))
			for key, value := range server.Headers {
				normalizedHeaders[key] = normalizeWindsurfMcpString(value)
			}
			server.Headers = normalizedHeaders
		}
		if server.Extra != nil {
			server.Extra = normalizeWindsurfMcpExtra(server.Extra).(map[string]any)
		}
	}
	return servers, diags, nil
}

func appendWindsurfMcpDiagnostics(diags *[]diag.Diagnostic, resource, file, key, original, normalized string) {
	if original != normalized {
		*diags = append(*diags, diag.Diagnostic{
			Code:     diag.CodeIngestWindsurfMcpEnvNormalized,
			Severity: diag.SeverityInfo,
			Resource: resource,
			Message:  fmt.Sprintf("Windsurf MCP env %q was normalized to canonical interpolation", key),
			Hint:     "Windsurf ${env:VAR} syntax was converted to canonical ${VAR} form during ingestion",
			Source:   diag.SourceRef{File: file, Line: 0},
		})
	}
	if looksLiteralPath(normalized) {
		*diags = append(*diags, diag.Diagnostic{
			Code:     diag.CodeIngestWindsurfLiteralPathPreserved,
			Severity: diag.SeverityInfo,
			Resource: resource,
			Message:  fmt.Sprintf("literal Windsurf path preserved for MCP env %q", key),
			Hint:     "Literal Windsurf paths remain unchanged during ingestion",
			Source:   diag.SourceRef{File: file, Line: 0},
		})
	}
}

// normalizeWindsurfMcpString converts Windsurf-specific interpolation patterns
// to canonical (Claude-compatible) IR form.
func normalizeWindsurfMcpString(value string) string {
	// Standalone ${env:VAR} → ${VAR}
	if m := windsurfEnvPattern.FindStringSubmatch(value); m != nil {
		return "${" + m[1] + "}"
	}
	// Embedded ${env:VAR} within a larger string → ${VAR}.
	if windsurfEmbeddedEnvPattern.MatchString(value) {
		return windsurfEmbeddedEnvPattern.ReplaceAllString(value, "${$1}")
	}
	// ${file:PATH} is already canonical and remains unchanged.
	return value
}

// normalizeWindsurfMcpExtra recursively normalizes JSON-like preserved fields.
func normalizeWindsurfMcpExtra(value any) any {
	switch value := value.(type) {
	case string:
		return normalizeWindsurfMcpString(value)
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, nested := range value {
			result[key] = normalizeWindsurfMcpExtra(nested)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, nested := range value {
			result[i] = normalizeWindsurfMcpExtra(nested)
		}
		return result
	default:
		return value
	}
}

// normalizeWindsurfEnvValue is retained for focused tests and compatibility.
func normalizeWindsurfEnvValue(value string) (string, bool) {
	normalized := normalizeWindsurfMcpString(value)
	return normalized, normalized != value
}

func looksLiteralPath(value string) bool {
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "${env:") || strings.HasPrefix(value, "${file:") || strings.HasPrefix(value, "${") {
		return false
	}
	return strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/") || strings.Contains(value, "/")
}
