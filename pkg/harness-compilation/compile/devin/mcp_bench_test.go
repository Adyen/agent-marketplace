package devin

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// MCP translation runs once per selected server on every sync, convert, and
// status invocation, so it sits on the same user-visible latency path as skill,
// subagent, and hook translation. It is the most field-heavy per-resource path
// in this target: it marshals JSON, walks two string maps, and runs a regex
// rewrite over every value.
//
// Each shape gets its own sub-benchmark so a regression in one cannot hide
// behind another: stdio is the common case, http and oauth exercise the remote
// form and the typed OAuth trio, interpolation exercises the regex rewrite, and
// the failure shapes exercise the skip branches that allocate a diagnostic
// instead of an artifact.

func BenchmarkTranslateMcpServer(b *testing.B) {
	source := ir.SourceRef{File: ".mcp.json", Line: 1}

	pluginRootOptions := compile.Options{}
	compile.WithPluginRoot("/plugins/demo")(&pluginRootOptions)

	cases := []struct {
		name    string
		server  *ir.McpServer
		options compile.Options
	}{
		{
			name:   "stdio-minimal",
			server: ir.NewMcpServer("demo", "npx", nil, nil, nil, source),
		},
		{
			name:   "stdio-rich",
			server: ir.NewMcpServer("demo", "npx", []string{"-y", "demo-mcp-server"}, map[string]string{"SETTING": "value", "MODE": "fast"}, nil, source),
		},
		{
			name: "http",
			server: ir.NewMcpServerWithOptions("remote", "", nil, nil, nil, source,
				ir.WithMcpURL("https://example.invalid/mcp"),
				ir.WithMcpTransport("http"),
				ir.WithMcpHeaders(map[string]string{"X-Client": "example"}),
				ir.WithMcpDisabled(false)),
		},
		{
			name: "oauth",
			server: ir.NewMcpServerWithOptions("remote", "", nil, nil, nil, source,
				ir.WithMcpURL("https://example.invalid/mcp"),
				ir.WithMcpTransport("http"),
				ir.WithMcpHeaders(map[string]string{"X-Client": "example"}),
				ir.WithMcpOAuth("client-id", "${env:OAUTH_CLIENT_SECRET}", "https://example.invalid")),
		},
		{
			name:   "interpolation",
			server: ir.NewMcpServer("demo", "npx", []string{"--token=${API_TOKEN}"}, map[string]string{"A": "${env:A_VALUE}", "B": "prefix-${B_VALUE}-suffix"}, nil, source),
		},
		{
			name:    "plugin-root",
			server:  ir.NewMcpServer("demo", "${CLAUDE_PLUGIN_ROOT}/bin/server", nil, map[string]string{"ENV_FILE": "${CLAUDE_PLUGIN_ROOT}/.env"}, nil, source),
			options: pluginRootOptions,
		},
		{
			name:   "extras",
			server: ir.NewMcpServer("demo", "npx", nil, nil, map[string]any{"cwd": "/srv", "timeoutMs": 1000, "note": "n"}, source),
		},
		{
			name: "transport-ambiguous",
			server: ir.NewMcpServerWithOptions("demo", "npx", nil, nil, nil, source,
				ir.WithMcpURL("https://example.invalid/mcp")),
		},
		{
			name:   "transport-incomplete",
			server: ir.NewMcpServer("demo", "", nil, nil, nil, source),
		},
		{
			name: "transport-unsupported",
			server: ir.NewMcpServerWithOptions("remote", "", nil, nil, nil, source,
				ir.WithMcpURL("https://example.invalid/mcp"),
				ir.WithMcpTransport("websocket")),
		},
		{
			name: "field-dropped",
			server: ir.NewMcpServerWithOptions("demo", "npx", nil, nil, nil, source,
				ir.WithMcpTransport("http"),
				ir.WithMcpHeaders(map[string]string{"X-Client": "example"}),
				ir.WithMcpOAuth("client-id", "${env:SECRET}", "https://example.invalid")),
		},
		{
			name:   "interpolation-unsupported",
			server: ir.NewMcpServer("demo", "npx", nil, map[string]string{"A": "${file:/etc/a}"}, nil, source),
		},
		{
			name:   "literal-credential",
			server: ir.NewMcpServer("demo", "npx", nil, map[string]string{"API_TOKEN": "literal-token-value"}, nil, source),
		},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := translateMcpServer(c.server, c.options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMergeMcpFragments measures the aggregate-merge side of the MCP path.
// It is the part that grows with the number of selected servers, so a keyed
// merge that became accidentally superlinear would show up here.
func BenchmarkMergeMcpFragments(b *testing.B) {
	source := ir.SourceRef{File: ".mcp.json", Line: 1}
	names := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}

	build := func(count int, duplicates bool) []artifact.Artifact {
		fragments := make([]artifact.Artifact, 0, count)
		for i := 0; i < count; i++ {
			name := names[i%len(names)]
			if duplicates {
				name = names[0]
			}
			server := ir.NewMcpServer(name, "npx", []string{"-y", name}, map[string]string{"SETTING": "value"}, nil, source)
			arts, _, err := translateMcpServer(server, compile.Options{})
			if err != nil {
				b.Fatal(err)
			}
			fragments = append(fragments, arts...)
		}
		return fragments
	}

	cases := []struct {
		name      string
		fragments []artifact.Artifact
	}{
		{name: "distinct-8", fragments: build(8, false)},
		{name: "identical-keys-64", fragments: build(64, true)},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := artifact.MergeFragments(c.fragments); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
