package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyncExitCodeForSkippedDevinAggregateEntries is the END-TO-END proof for
// the second half of the promoted plan's error-behavior sentence: "invalid or
// conflicting desired entries are skipped while other valid entries are patched
// atomically; sync still exits non-zero" (bead agent-marketplace-5exj.20).
//
// It deliberately exercises the whole path rather than a severity constant:
//
//	compile/devin        emits an error diagnostic for the skipped entry
//	internal/materialize folds it into Report.Diags
//	internal/pipeline    promotes error diagnostics into SyncResult.Errors
//	cmd/.../sync.go      joins them into the RunE error, which cobra turns
//	                     into a non-zero process exit
//
// The failing cases cover both Devin aggregates and every entry-removing
// reason. Each asserts BOTH halves at once: the command fails, and the valid
// siblings still reached disk.
//
// Bead agent-marketplace-5exj.23 added the interpolation cases, including the
// deliberate NEGATIVE control (wantSuccess): the same unrepresentable
// interpolation style in a value the entry can live without must still exit
// ZERO, because the entry is written. Without that row the table would pass
// just as happily if every interpolation loss had been escalated, which is the
// outcome the split exists to prevent.
func TestSyncExitCodeForSkippedDevinAggregateEntries(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name string
		// resource is the extra registry content that cannot be represented.
		hooksJSON string
		mcpJSON   string
		// selection is the config block selecting the resources.
		selection string
		// wantSuccess inverts the assertion: the loss is value-level, so sync
		// must exit zero and the entry must survive without the bad value.
		wantSuccess bool
		wantCode    string
		// wantPresent are output-relative paths that must exist anyway.
		wantAggregate string
		wantContains  string
		// wantOmits are substrings the aggregate must NOT contain.
		wantOmits []string
	}{
		{
			name: "an unrepresentable hook event fails the sync and keeps the valid hook",
			hooksJSON: `{
  "PreToolUse": [{"hooks": [{"type": "command", "command": "guard.sh"}]}],
  "SubagentStop": [{"hooks": [{"type": "command", "command": "never.sh"}]}]
}`,
			selection: `      - hook/PreToolUse-command
      - hook/SubagentStop-command
`,
			wantCode:      "hook.devin.event-unmappable",
			wantAggregate: filepath.Join(".devin", "hooks.v1.json"),
			wantContains:  "guard.sh",
			wantOmits:     []string{"SubagentStop", "never.sh"},
		},
		{
			name: "an unrepresentable MCP transport fails the sync and keeps the valid server",
			mcpJSON: `{
  "mcpServers": {
    "good": {"command": "npx"},
    "bad": {"url": "https://example.invalid/mcp", "transport": "websocket"}
  }
}`,
			selection: `      - mcp/good
      - mcp/bad
`,
			wantCode:      "mcp.devin.transport-unsupported",
			wantAggregate: filepath.Join(".devin", "mcp_config.json"),
			wantContains:  `"good"`,
			wantOmits:     []string{`"bad"`, "websocket"},
		},
		{
			// Bead agent-marketplace-5exj.23. The unrepresentable value is the
			// server's command, which the stdio form cannot do without, so the
			// whole entry is dropped. Before .23 this path emitted the shared
			// mcp.interpolation-unsupported WARNING and the sync exited 0
			// having silently lost an MCP server; it now emits the entry-level
			// error code and fails the run.
			name: "an unrepresentable MCP command fails the sync and keeps the valid server",
			mcpJSON: `{
  "mcpServers": {
    "good": {"command": "npx"},
    "bad": {"command": "${file:/etc/mcp-command}"}
  }
}`,
			selection: `      - mcp/good
      - mcp/bad
`,
			wantCode:      "mcp.devin.interpolation-unrepresentable",
			wantAggregate: filepath.Join(".devin", "mcp_config.json"),
			wantContains:  `"good"`,
			wantOmits:     []string{`"bad"`, "mcp-command"},
		},
		{
			// The remote-form half of the same rule: the url is the field a
			// remote server cannot do without.
			name: "an unrepresentable MCP url fails the sync and keeps the valid server",
			mcpJSON: `{
  "mcpServers": {
    "good": {"command": "npx"},
    "bad": {"url": "${file:/etc/mcp-endpoint}"}
  }
}`,
			selection: `      - mcp/good
      - mcp/bad
`,
			wantCode:      "mcp.devin.interpolation-unrepresentable",
			wantAggregate: filepath.Join(".devin", "mcp_config.json"),
			wantContains:  `"good"`,
			wantOmits:     []string{`"bad"`, "mcp-endpoint"},
		},
		{
			// The contrast case, and the reason the shared code was split
			// rather than escalated: the SAME unrepresentable style in an env
			// value costs one env key, the entry is still written, and the
			// sync exits 0. Escalating mcp.interpolation-unsupported would
			// have deleted this server instead.
			name: "an unrepresentable MCP env value keeps the entry and exits zero",
			mcpJSON: `{
  "mcpServers": {
    "good": {"command": "npx", "env": {"KEEP": "plain", "DROP": "${file:/etc/mcp-token}"}}
  }
}`,
			selection: `      - mcp/good
`,
			wantSuccess:   true,
			wantAggregate: filepath.Join(".devin", "mcp_config.json"),
			wantContains:  `"KEEP"`,
			wantOmits:     []string{`"DROP"`, "mcp-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

			origin, working := createBareRepoWithWorkingClone(t, "devin-skip.git")
			writeSkill(t, working, "keeper", "Survives the skip", nil)
			if tt.hooksJSON != "" {
				writeFile(t, filepath.Join(working, "hooks", "hooks.json"), tt.hooksJSON)
			}
			if tt.mcpJSON != "" {
				writeFile(t, filepath.Join(working, ".mcp.json"), tt.mcpJSON)
			}
			commitAndPushAll(t, working, "Add devin fixtures")

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - devin
sources:
  resources:
    git: file://`+origin+`
    ref: main
resources:
  resources:
    include:
      - skill/keeper
`+tt.selection)

			withWorkingDir(t, repoRoot)

			output, err := executeCommand("sync")
			switch {
			case tt.wantSuccess:
				if err != nil {
					t.Fatalf("sync exited non-zero for a value-level loss that kept its entry: %v\noutput:\n%s", err, output)
				}
			default:
				if err == nil {
					t.Fatalf("sync exited 0 after skipping a Devin aggregate entry; output:\n%s", output)
				}
				if !strings.Contains(err.Error(), tt.wantCode) {
					t.Errorf("sync error = %v, want it to name %s", err, tt.wantCode)
				}
				if !strings.Contains(output, "Errors 1") {
					t.Errorf("sync summary does not report the error; output:\n%s", output)
				}
			}

			// The valid siblings still reached disk: suppression is per entry,
			// not per target.
			skillLink := filepath.Join(repoRoot, ".devin", "skills", "keeper")
			if _, statErr := os.Lstat(skillLink); statErr != nil {
				t.Errorf("a valid skill was lost because a sibling entry was skipped: %v", statErr)
			}
			aggregate := filepath.Join(repoRoot, tt.wantAggregate)
			data, readErr := os.ReadFile(aggregate)
			if readErr != nil {
				t.Fatalf("read %s: %v", tt.wantAggregate, readErr)
			}
			if !strings.Contains(string(data), tt.wantContains) {
				t.Errorf("%s does not carry the valid entry:\n%s", tt.wantAggregate, data)
			}
			for _, omitted := range tt.wantOmits {
				if strings.Contains(string(data), omitted) {
					t.Errorf("%s still carries %q, which could not be represented:\n%s", tt.wantAggregate, omitted, data)
				}
			}
		})
	}
}
