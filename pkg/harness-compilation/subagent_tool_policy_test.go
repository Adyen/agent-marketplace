package harnesscompile

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	toml "github.com/pelletier/go-toml/v2"

	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
)

// spec: harness-compilation/same-harness-native-allowlists-survive-conversion
func TestNativeSubagentToolPolicyMatrix(t *testing.T) {
	t.Parallel()
	for _, from := range matrixHarnesses {
		for _, to := range matrixHarnesses {
			t.Run(string(from)+"-to-"+string(to), func(t *testing.T) {
				t.Parallel()
				tools := []string{"Read", "Grep", "native_reader___lookup"}
				if from == HarnessFactory {
					tools = []string{"Read", "LS", "FetchUrl", "ToolSearch", "native_reader___lookup", "Create", "Edit"}
				}
				if from == HarnessDevin {
					tools = []string{"read", "grep"}
				}
				src := nativeToolPolicyFixture(from, tools)
				result, err := Convert(src, from, to)
				if err != nil {
					t.Fatal(err)
				}
				if from == HarnessWindsurf {
					if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, diag.CodeIngestWindsurfSubagentViaAgentsMDSkipped) {
						t.Fatalf("unsupported source invented a subagent: %+v", result)
					}
					return
				}
				if from == HarnessCodex {
					// Codex has sandbox policy, not a native tool allowlist.
					if to != TargetCodex {
						requireToolPolicyRefusal(t, result)
					} else {
						if result.HasErrors() || len(result.Artifacts) != 1 {
							t.Fatalf("native Codex sandbox policy rejected: %+v", result)
						}
						var native map[string]any
						if err := toml.Unmarshal(result.Artifacts[0].Bytes, &native); err != nil || native["sandbox_mode"] != "read-only" {
							t.Fatalf("native Codex sandbox policy changed: %#v, %v", native, err)
						}
					}
					return
				}
				if to == TargetWindsurf {
					if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, diag.CodeSubagentWindsurfNoPrimitive) {
						t.Fatalf("unsupported target emitted a subagent: %+v", result)
					}
					return
				}
				if from == to {
					requireRenderedTools(t, result, tools, to)
					return
				}
				if from == HarnessDevin && to != TargetCodex {
					requireRenderedTools(t, result, []string{"Read", "Grep"}, to)
					return
				}
				switch to {
				case TargetFactory, TargetCursor, TargetGemini, TargetCodex:
					requireToolPolicyRefusal(t, result)
				}
			})
		}
	}
}

// spec: harness-compilation/empty-and-absent-native-policies-remain-distinct
func TestNativeSubagentEmptyAndAbsentTools(t *testing.T) {
	t.Parallel()
	for _, from := range []Harness{HarnessClaude, HarnessFactory, HarnessCursor, HarnessGemini, HarnessDevin} {
		for _, tools := range [][]string{nil, {}} {
			name := "empty"
			if tools == nil {
				name = "absent"
			}
			t.Run(string(from)+"/"+name, func(t *testing.T) {
				t.Parallel()
				result, err := Convert(nativeToolPolicyFixture(from, tools), from, from)
				if err != nil {
					t.Fatal(err)
				}
				requireRenderedTools(t, result, tools, from)
			})
		}
	}
}

func TestSupportedSubagentToolPolicyMatrix(t *testing.T) {
	t.Parallel()
	for _, from := range []Harness{HarnessClaude, HarnessFactory, HarnessCursor, HarnessGemini, HarnessDevin} {
		for _, to := range matrixHarnesses {
			t.Run(string(from)+"-to-"+string(to), func(t *testing.T) {
				t.Parallel()
				tools := []string{"Read", "Grep", "Glob"}
				if from == HarnessDevin {
					tools = []string{"read", "grep", "glob"}
				}
				result, err := Convert(nativeToolPolicyFixture(from, tools), from, to)
				if err != nil {
					t.Fatal(err)
				}
				switch to {
				case TargetCodex:
					requireToolPolicyRefusal(t, result)
				case TargetWindsurf:
					if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, diag.CodeSubagentWindsurfNoPrimitive) {
						t.Fatalf("unsupported target emitted a subagent: %+v", result)
					}
				default:
					want := []string{"Read", "Grep", "Glob"}
					if to == TargetDevin {
						want = []string{"read", "grep", "glob"}
					}
					requireRenderedTools(t, result, want, to)
				}
			})
		}
	}
}

func TestForeignNativeToolPolicyHonorsExplicitOverride(t *testing.T) {
	t.Parallel()
	bundle, err := IngestBundle(nativeToolPolicyFixture(HarnessFactory, []string{"Read", "native_reader___lookup"}), HarnessFactory)
	if err != nil || len(bundle.Resources) != 1 {
		t.Fatalf("IngestBundle: %v %+v", err, bundle)
	}
	for _, to := range []Target{TargetFactory, TargetCursor, TargetGemini, TargetDevin} {
		t.Run(string(to), func(t *testing.T) {
			t.Parallel()
			mapped := "Grep"
			if to == TargetDevin {
				mapped = "grep"
			}
			result, err := compilepkg.Compile(bundle.Resources[0], to, compilepkg.WithToolOverride(map[string]string{"native_reader___lookup": mapped}))
			if err != nil || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("explicit override rejected: %v %+v", err, result)
			}
			public := &Result{Artifacts: result.Artifacts, Diagnostics: result.Diagnostics}
			want := []string{"Read", mapped}
			if to == TargetDevin {
				want[0] = "read"
			}
			requireRenderedTools(t, public, want, to)
		})
	}
}

func nativeToolPolicyFixture(from Harness, tools []string) fstest.MapFS {
	var b strings.Builder
	b.WriteString("---\nname: reader\ndescription: Reader\nmodel: sonnet\n")
	key := "tools"
	if from == HarnessDevin {
		key = "allowed-tools"
	}
	if tools != nil {
		if len(tools) == 0 {
			b.WriteString(key + ": []\n")
		} else {
			b.WriteString(key + ":\n")
			for _, tool := range tools {
				b.WriteString("  - " + tool + "\n")
			}
		}
	}
	b.WriteString("---\nRead only.\n")
	path := map[Harness]string{
		HarnessClaude: ".claude/agents/reader.md", HarnessFactory: ".factory/droids/reader.md",
		HarnessCursor: ".cursor/agents/reader.md", HarnessGemini: ".gemini/agents/reader.md",
		HarnessDevin: ".devin/agents/reader.md", HarnessWindsurf: "AGENTS.md",
	}[from]
	if from == HarnessCodex {
		return fstest.MapFS{".codex/agents/reader.toml": {Data: []byte("name = \"reader\"\ndescription = \"Reader\"\ndeveloper_instructions = \"Read only.\"\nsandbox_mode = \"read-only\"\n")}}
	}
	return fstest.MapFS{path: {Data: []byte(b.String())}}
}

func requireToolPolicyRefusal(t *testing.T, result *Result) {
	t.Helper()
	if len(result.Artifacts) != 0 || !result.HasErrors() {
		t.Fatalf("unrepresentable tool policy emitted an artifact or lacked an error: %+v", result)
	}
}

func requireRenderedTools(t *testing.T, result *Result, want []string, to Target) {
	t.Helper()
	if result.HasErrors() {
		t.Fatalf("representable policy rejected: %+v", result.Diagnostics)
	}
	var data []byte
	for _, art := range result.Artifacts {
		if strings.HasSuffix(art.Path, "/reader.md") {
			if data != nil {
				t.Fatal("multiple reader artifacts")
			}
			data = art.Bytes
		}
	}
	if data == nil {
		t.Fatalf("no reader artifact: %+v", result.Artifacts)
	}
	fm, _, err := yamlfm.ParseYAMLFrontmatter(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	key := "tools"
	if to == TargetDevin {
		key = "allowed-tools"
	}
	value, present := fm[key]
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
