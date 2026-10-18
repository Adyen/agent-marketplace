package harnesscompile

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
)

func TestNativeSubagentToolPolicyConversion(t *testing.T) {
	t.Parallel()
	for _, native := range []struct {
		harness   Harness
		path, key string
		tools     []string
	}{
		{HarnessFactory, ".factory/droids/reader.md", "tools", []string{"Read", "LS", "FetchUrl", "ToolSearch", "mcp__reader__lookup", "Create", "Edit"}},
		{HarnessClaude, ".claude/agents/reader.md", "tools", []string{"Read", "Grep", "mcp__reader__lookup"}},
		{HarnessCursor, ".cursor/agents/reader.md", "tools", []string{"Read", "Grep", "MCP:lookup"}},
		{HarnessGemini, ".gemini/agents/reader.md", "tools", []string{"read_file", "grep_search", "mcp_reader_lookup", "mcp_*"}},
		{HarnessDevin, ".devin/agents/reader.md", "allowed-tools", []string{"read", "grep", "glob"}},
	} {
		for _, policy := range []struct {
			name  string
			tools []string
		}{
			{"native", native.tools},
			{"empty", []string{}},
			{"absent", nil},
		} {
			t.Run(string(native.harness)+"/"+policy.name, func(t *testing.T) {
				t.Parallel()
				body := "---\nname: reader\ndescription: Reader\n"
				if policy.tools != nil {
					body += native.key + ": [" + strings.Join(policy.tools, ", ") + "]\n"
				}
				body += "---\nRead only.\n"
				src := fstest.MapFS{native.path: {Data: []byte(body)}}
				result, err := Convert(src, native.harness, native.harness)
				if err != nil {
					t.Fatal(err)
				}
				requireNativeSubagentTools(t, result, native.key, policy.tools)

				bundle, err := IngestBundle(src, native.harness)
				if err != nil || len(bundle.Resources) != 1 {
					t.Fatalf("IngestBundle: %v %+v", err, bundle)
				}
				encoded, err := artifact.FromIR(bundle.Resources[0])
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := artifact.DecodeResource("subagent", encoded.Payload)
				if err != nil {
					t.Fatal(err)
				}
				archived, err := Compile(decoded, native.harness)
				if err != nil {
					t.Fatal(err)
				}
				requireNativeSubagentTools(t, archived, native.key, policy.tools)
			})
		}
	}
}

func TestFactoryPluginSubagentToolPolicyConversion(t *testing.T) {
	t.Parallel()
	const body = "---\nname: reader\ndescription: Reader\nmodel: inherit\ntools: [Read, Grep, Glob, LS, FetchUrl, ToolSearch, mcp__reader__lookup]\n---\nRead only.\n"
	src := fstest.MapFS{
		".factory-plugin/plugin.json": {Data: []byte(`{"name":"reader-plugin","version":"1.0.0"}`)},
		"droids/reader.md":            {Data: []byte(body)},
	}
	result, err := Convert(src, HarnessFactory, TargetFactory)
	if err != nil {
		t.Fatal(err)
	}
	requireNativeSubagentTools(t, result, "tools", []string{"Read", "Grep", "Glob", "LS", "FetchUrl", "ToolSearch", "mcp__reader__lookup"})
	if got := string(src["droids/reader.md"].Data); got != body {
		t.Fatal("conversion changed its source policy")
	}
}

func requireNativeSubagentTools(t *testing.T, result *Result, key string, want []string) {
	t.Helper()
	if result.HasErrors() {
		t.Fatalf("native policy rejected: %+v", result.Diagnostics)
	}
	var data []byte
	for _, output := range result.Artifacts {
		if strings.HasSuffix(output.Path, "/reader.md") {
			if data != nil {
				t.Fatal("multiple reader artifacts")
			}
			data = output.Bytes
		}
	}
	if data == nil {
		t.Fatalf("no reader artifact: %+v", result.Artifacts)
	}
	fields, _, err := yamlfm.ParseYAMLFrontmatter(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	value, present := fields[key]
	if want == nil {
		if present {
			t.Fatalf("absent policy gained %s: %#v", key, value)
		}
		return
	}
	expected := make([]any, len(want))
	for i, tool := range want {
		expected[i] = tool
	}
	if !present || !reflect.DeepEqual(value, expected) {
		t.Fatalf("%s = %#v, want exactly %#v", key, value, expected)
	}
}
