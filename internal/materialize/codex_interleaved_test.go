package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	toml "github.com/pelletier/go-toml/v2"
)

const (
	interleavedTable      = "[profiles.safe]\nhandwritten = true\n\n"
	interleavedArrayTable = "[[profiles.list]]\nname = 'kept'\n\n"
)

func interleaveCodexMCP(t *testing.T, installed, handwritten string) string {
	t.Helper()
	envHeader := strings.Index(installed, `[mcp_servers."foo".env]`)
	if envHeader < 0 {
		t.Fatalf("installed configuration has no foo env table:\n%s", installed)
	}
	return installed[:envHeader] + handwritten + installed[envHeader:]
}

func codexMCPServerValue(t *testing.T, raw []byte, name string) map[string]any {
	t.Helper()
	var document map[string]any
	if err := toml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("reconciled configuration is not valid TOML: %v\n%s", err, raw)
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	server, _ := servers[name].(map[string]any)
	return server
}

// spec: owned-entry-reconciliation/interleaved-handwritten-tables-survive-owned-entry-sync
func TestRunPreservesHandwrittenTablesInterleavedWithOwnedCodexMCP(t *testing.T) {
	for _, handwritten := range []string{interleavedTable, interleavedArrayTable} {
		for _, change := range []string{"update", "retire"} {
			t.Run(strings.Fields(handwritten)[0]+"/"+change, func(t *testing.T) {
				repo, home := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				server := func(command string) resolver.ResolvedArtifact {
					return codexResource("foo", ir.NewMcpServer("foo", command, nil, map[string]string{"K": "v"}, nil, ir.SourceRef{}), config.RepoCommitted)
				}
				keep := codexResource("keep", ir.NewMcpServer("keep", "inert-keep", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted)
				report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{server("inert-a"), keep})
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, report.Diags)
				abs := filepath.Join(repo, ".codex", codexMCPFile)
				installed, err := os.ReadFile(abs)
				if err != nil {
					t.Fatal(err)
				}
				rewritten := interleaveCodexMCP(t, string(installed), handwritten)
				if err := os.WriteFile(abs, []byte(rewritten), 0o644); err != nil {
					t.Fatal(err)
				}

				next := []resolver.ResolvedArtifact{keep}
				if change == "update" {
					next = append(next, server("inert-b"))
				}
				report, err = Run(context.Background(), repo, next)
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, report.Diags)
				after, err := os.ReadFile(abs)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(after), handwritten) {
					t.Fatalf("handwritten table was not preserved byte-for-byte:\n%s", after)
				}
				foo := codexMCPServerValue(t, after, "foo")
				switch change {
				case "update":
					env, _ := foo["env"].(map[string]any)
					if foo["command"] != "inert-b" || env["K"] != "v" || strings.Contains(string(after), "inert-a") {
						t.Fatalf("owned server update not applied: %#v\n%s", foo, after)
					}
				case "retire":
					if foo != nil || strings.Contains(string(after), `"foo"`) {
						t.Fatalf("owned server not pruned: %#v\n%s", foo, after)
					}
				}
				if keepValue := codexMCPServerValue(t, after, "keep"); keepValue["command"] != "inert-keep" {
					t.Fatalf("sibling owned server changed: %#v\n%s", keepValue, after)
				}
			})
		}
	}
}

// spec: owned-entry-reconciliation/interleaved-handwritten-tables-survive-owned-entry-removal-and-replacement
func TestCodexMCPRepairEditsPreserveInterleavedHandwrittenTables(t *testing.T) {
	raw := []byte("[mcp_servers.\"foo\"]\ncommand = 'inert-a'\n\n" + interleavedTable + "[mcp_servers.\"foo\".env]\nK = 'v'\n\n[mcp_servers.\"keep\"]\ncommand = 'inert-keep'\n")
	sections, _, err := codexMCPSections(raw)
	if err != nil {
		t.Fatal(err)
	}
	state := &codexMCPRepairState{raw: raw, sections: sections}
	id := ownership.EntryID{Kind: ownership.KindMcpServer, Container: codexMCPContainer, Key: "foo"}

	removed, err := state.remove(id, sections["foo"].value.ValueDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(removed), interleavedTable) || codexMCPServerValue(t, removed, "foo") != nil || codexMCPServerValue(t, removed, "keep") == nil {
		t.Fatalf("removal did not delete only the owned server:\n%s", removed)
	}

	replaced, err := state.replace(id, []byte("[mcp_servers.\"foo\"]\ncommand = 'inert-b'\n"))
	if err != nil {
		t.Fatal(err)
	}
	foo := codexMCPServerValue(t, replaced, "foo")
	if !strings.Contains(string(replaced), interleavedTable) || foo["command"] != "inert-b" || foo["env"] != nil || codexMCPServerValue(t, replaced, "keep") == nil {
		t.Fatalf("replacement did not replace only the owned server: %#v\n%s", foo, replaced)
	}
}
