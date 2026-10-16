package harnesscompile

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	mcpFenceNamespace = "_agent-marketplace"
	mcpFenceDocMarker = "PUBLISHING-DOC-MARKER-7f3a"
	mcpFenceStdioCmd  = "npx"
	mcpFenceRemoteURL = "https://mcp.example.test/docs"
)

func mcpFenceValidMetadata() map[string]any {
	return map[string]any{
		"description":    "Docs server",
		"readme":         "README.md",
		"readme_content": "# Docs\n" + mcpFenceDocMarker + "\n",
	}
}

func mcpFenceServer(transport string, extra map[string]any) *ir.McpServer {
	if transport == "remote" {
		return ir.NewMcpServerWithOptions("docs-remote", "", nil, nil, extra, ir.SourceRef{}, ir.WithMcpURL(mcpFenceRemoteURL), ir.WithMcpHeaders(map[string]string{"X-Trace": "fixed"}))
	}
	return ir.NewMcpServer("docs-stdio", mcpFenceStdioCmd, []string{"-y", "docs-mcp"}, map[string]string{"DOCS_TOKEN": "${DOCS_TOKEN}"}, extra, ir.SourceRef{})
}

func mcpFenceStripNamespace(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(extra))
	for key, value := range extra {
		if strings.EqualFold(key, mcpFenceNamespace) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mcpFenceDeepCopy(t *testing.T, extra map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(extra)
	if err != nil {
		t.Fatalf("marshal extra snapshot: %v", err)
	}
	var copy map[string]any
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatalf("unmarshal extra snapshot: %v", err)
	}
	return copy
}

type mcpFenceFile struct {
	bytes []byte
	mode  fs.FileMode
}

func mcpFenceTree(t *testing.T, root string) map[string]mcpFenceFile {
	t.Helper()
	tree := map[string]mcpFenceFile{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = mcpFenceFile{bytes: data, mode: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", root, err)
	}
	return tree
}

// spec: mcp-publishing-metadata-native-fence/every-target-consumes-the-reserved-namespace
// spec: mcp-publishing-metadata-native-fence/malformed-and-case-variant-entries-cannot-bypass-exclusion
func TestMcpPublishingNamespaceFencedAtNativeSerialization(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
	}{
		{"valid-object", map[string]any{mcpFenceNamespace: mcpFenceValidMetadata()}},
		{"empty-object", map[string]any{mcpFenceNamespace: map[string]any{}}},
		{"null", map[string]any{mcpFenceNamespace: nil}},
		{"scalar", map[string]any{mcpFenceNamespace: "malformed-scalar"}},
		{"array", map[string]any{mcpFenceNamespace: []any{"forged"}}},
		{"case-variant", map[string]any{"_Agent-Marketplace": mcpFenceValidMetadata()}},
		{"multiple-variants", map[string]any{mcpFenceNamespace: mcpFenceValidMetadata(), "_AGENT-MARKETPLACE": "forged"}},
		{"with-unrelated", map[string]any{mcpFenceNamespace: mcpFenceValidMetadata(), "x-partner-extension": map[string]any{"keep": true}, "description": "Native bare description"}},
	}
	for _, tc := range cases {
		for _, target := range Targets() {
			for _, scope := range []Scope{ScopeProject, ScopeUser} {
				for _, transport := range []string{"stdio", "remote"} {
					t.Run(tc.name+"/"+string(target)+"/"+transport+"/"+map[Scope]string{ScopeProject: "project", ScopeUser: "user"}[scope], func(t *testing.T) {
						baselineExtra := mcpFenceStripNamespace(tc.extra)
						withNs := mcpFenceServer(transport, tc.extra)
						baseline := mcpFenceServer(transport, baselineExtra)

						snapshot := mcpFenceDeepCopy(t, tc.extra)

						got, err := Compile(withNs, target, WithScope(scope))
						if err != nil {
							t.Fatalf("Compile with namespace: %v", err)
						}
						want, err := Compile(baseline, target, WithScope(scope))
						if err != nil {
							t.Fatalf("Compile baseline: %v", err)
						}

						if !reflect.DeepEqual(withNs.Extra, snapshot) {
							t.Fatalf("compile mutated the preserved extension map:\nbefore: %#v\nafter:  %#v", snapshot, withNs.Extra)
						}
						if len(got.Artifacts) == 0 || len(want.Artifacts) == 0 {
							t.Fatalf("compatible %s %s fixture produced no artifacts: got=%d want=%d diags=%+v", transport, scope, len(got.Artifacts), len(want.Artifacts), got.Diagnostics)
						}
						if !reflect.DeepEqual(got.Artifacts, want.Artifacts) {
							t.Fatalf("publishing namespace changed native artifacts:\nwith:    %+v\nwithout: %+v", got.Artifacts, want.Artifacts)
						}
						if !reflect.DeepEqual(got.Diagnostics, want.Diagnostics) {
							t.Fatalf("publishing namespace changed diagnostics:\nwith:    %+v\nwithout: %+v", got.Diagnostics, want.Diagnostics)
						}
						if !reflect.DeepEqual(got.Skipped, want.Skipped) {
							t.Fatalf("publishing namespace changed skipped resources:\nwith:    %+v\nwithout: %+v", got.Skipped, want.Skipped)
						}

						var all []byte
						for _, art := range got.Artifacts {
							all = append(all, art.Bytes...)
						}
						if bytes.Contains(bytes.ToLower(all), []byte(mcpFenceNamespace)) || bytes.Contains(all, []byte(mcpFenceDocMarker)) {
							t.Fatalf("publishing metadata leaked into %s %s artifacts:\n%s", transport, target, all)
						}
						connection := mcpFenceStdioCmd
						if transport == "remote" {
							connection = mcpFenceRemoteURL
						}
						if !bytes.Contains(all, []byte(connection)) {
							t.Fatalf("valid %s connection dropped for %s:\n%s", transport, target, all)
						}

						if target == TargetDevin || target == TargetCodex {
							return
						}
						gotDir, wantDir := t.TempDir(), t.TempDir()
						gotReport, gotErr := Materialize(got, gotDir)
						wantReport, wantErr := Materialize(want, wantDir)
						if gotErr != nil || wantErr != nil {
							t.Fatalf("materialize failed: with=%v without=%v", gotErr, wantErr)
						}
						if !reflect.DeepEqual(gotReport.Written, wantReport.Written) {
							t.Fatalf("materialize report differs:\nwith:    %+v\nwithout: %+v", gotReport, wantReport)
						}
						gotTree, wantTree := mcpFenceTree(t, gotDir), mcpFenceTree(t, wantDir)
						if len(gotTree) == 0 || len(wantTree) == 0 {
							t.Fatalf("materialize wrote nothing: with=%d without=%d", len(gotTree), len(wantTree))
						}
						if !reflect.DeepEqual(gotTree, wantTree) {
							t.Fatalf("materialized tree differs:\nwith:    %+v\nwithout: %+v", gotTree, wantTree)
						}
						for rel, file := range gotTree {
							if strings.HasPrefix(strings.ToUpper(filepath.Base(rel)), "README") {
								t.Fatalf("publishing README materialized at %q", rel)
							}
							if bytes.Contains(bytes.ToLower(file.bytes), []byte(mcpFenceNamespace)) || bytes.Contains(file.bytes, []byte(mcpFenceDocMarker)) {
								t.Fatalf("publishing metadata materialized at %q:\n%s", rel, file.bytes)
							}
						}
					})
				}
			}
		}
	}
}

// spec: mcp-publishing-metadata-native-fence/every-target-consumes-the-reserved-namespace
func TestMcpPublishingNamespaceLeavesUnrelatedExtensionsAndNonMcpKinds(t *testing.T) {
	server := mcpFenceServer("stdio", map[string]any{"x-native-passthrough": map[string]any{"keep": true}, "description": "Native bare description"})
	result, err := Compile(server, TargetClaude, WithScope(ScopeProject))
	if err != nil || len(result.Artifacts) == 0 {
		t.Fatalf("compile: %v %+v", err, result)
	}
	if !bytes.Contains(result.Artifacts[0].Bytes, []byte("x-native-passthrough")) {
		t.Fatalf("unrelated MCP extension was not preserved for a JSON target:\n%s", result.Artifacts[0].Bytes)
	}
	if !bytes.Contains(result.Artifacts[0].Bytes, []byte(`"description":"Native bare description"`)) {
		t.Fatalf("bare native description was not preserved for a JSON target:\n%s", result.Artifacts[0].Bytes)
	}

	command := ir.NewCommand("publish-note", "Note", "Body", map[string]any{mcpFenceNamespace: mcpFenceValidMetadata()}, ir.SourceRef{})
	out, err := Compile(command, TargetClaude, WithScope(ScopeProject))
	if err != nil || len(out.Artifacts) == 0 {
		t.Fatalf("compile command: %v %+v", err, out)
	}
	if !bytes.Contains(out.Artifacts[0].Bytes, []byte(mcpFenceNamespace)) || !bytes.Contains(out.Artifacts[0].Bytes, []byte(mcpFenceDocMarker)) {
		t.Fatalf("non-MCP resource lost the preserved extension namespace:\n%s", out.Artifacts[0].Bytes)
	}
}
