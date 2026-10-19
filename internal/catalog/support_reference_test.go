package catalog

import "testing"

// spec: hook-resources/a-reference-to-a-missing-support-file-refuses-the-hook
// spec: hook-resources/bare-and-dynamic-plugin-root-references-are-not-checked
// spec: hook-resources/legacy-folder-relative-references-keep-working-with-a-warning
// spec: hook-resources/references-that-resolve-under-both-meanings-use-the-bundle-root
func TestCheckHookSupportReferences(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		installed    []string
		legacyPrefix string
		wantCommand  string
		wantMissing  bool
		wantLegacy   bool
	}{
		{
			name:        "bundle hit",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh`,
		},
		{
			name:        "directory prefix hit",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks`,
			installed:   []string{"hooks/scripts/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks`,
		},
		{
			name:         "legacy hit rewrites",
			command:      `${CLAUDE_PLUGIN_ROOT}/check.sh`,
			installed:    []string{"hooks/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh`,
			wantLegacy:   true,
		},
		{
			name:         "both meanings prefer bundle",
			command:      `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh`,
			installed:    []string{"hooks/check.sh", "hooks/hooks/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh`,
		},
		{
			name:         "missing under both meanings",
			command:      `${CLAUDE_PLUGIN_ROOT}/absent.sh`,
			installed:    []string{"hooks/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `${CLAUDE_PLUGIN_ROOT}/absent.sh`,
			wantMissing:  true,
		},
		{
			name:        "bare token unchecked",
			command:     `cd "${CLAUDE_PLUGIN_ROOT}"`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `cd "${CLAUDE_PLUGIN_ROOT}"`,
		},
		{
			name:        "dollar expansion in path unchecked",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/$SCRIPT`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/$SCRIPT`,
		},
		{
			name:        "glob in path unchecked",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/*.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/*.sh`,
		},
		{
			name:         "single-quoted token untouched",
			command:      `echo '${CLAUDE_PLUGIN_ROOT}/absent.sh'`,
			installed:    []string{"hooks/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `echo '${CLAUDE_PLUGIN_ROOT}/absent.sh'`,
		},
		{
			name:         "unbraced token checked",
			command:      `run $CLAUDE_PLUGIN_ROOT/absent.sh`,
			installed:    []string{"hooks/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `run $CLAUDE_PLUGIN_ROOT/absent.sh`,
			wantMissing:  true,
		},
		{
			name:        "empty legacy prefix disables fallback",
			command:     `${CLAUDE_PLUGIN_ROOT}/check.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/check.sh`,
			wantMissing: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CheckHookSupportReferences(tc.command, tc.installed, tc.legacyPrefix)
			if err != nil {
				t.Fatalf("CheckHookSupportReferences: %v", err)
			}
			if result.Command != tc.wantCommand || result.Missing != tc.wantMissing || result.Legacy != tc.wantLegacy {
				t.Fatalf("got %+v, want command %q missing=%t legacy=%t", result, tc.wantCommand, tc.wantMissing, tc.wantLegacy)
			}
		})
	}
}
