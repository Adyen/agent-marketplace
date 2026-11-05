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
			name:        "dollar expansion after literal prefix unchecked",
			command:     `${CLAUDE_PLUGIN_ROOT}/missing/$SCRIPT`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/missing/$SCRIPT`,
		},
		{
			name:        "command substitution after literal prefix unchecked",
			command:     `${CLAUDE_PLUGIN_ROOT}/missing/$(pick)`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/missing/$(pick)`,
		},
		{
			name:        "dot-cleaned bundle path resolves",
			command:     `${CLAUDE_PLUGIN_ROOT}/./hooks/check.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/./hooks/check.sh`,
		},
		{
			name:        "dotdot-internal bundle path resolves",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/../hooks/check.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/../hooks/check.sh`,
		},
		{
			name:         "escaping path is missing",
			command:      `${CLAUDE_PLUGIN_ROOT}/../outside.sh`,
			installed:    []string{"hooks/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `${CLAUDE_PLUGIN_ROOT}/../outside.sh`,
			wantMissing:  true,
		},
		{
			name:         "dot-cleaned path resolves via legacy",
			command:      `${CLAUDE_PLUGIN_ROOT}/./scripts/check.sh`,
			installed:    []string{"hooks/scripts/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `${CLAUDE_PLUGIN_ROOT}/hooks/./scripts/check.sh`,
			wantLegacy:   true,
		},
		{
			name:        "quoted path with parens resolves",
			command:     `"${CLAUDE_PLUGIN_ROOT}/hooks/a(1).sh"`,
			installed:   []string{"hooks/a(1).sh"},
			wantCommand: `"${CLAUDE_PLUGIN_ROOT}/hooks/a(1).sh"`,
		},
		{
			name:        "quoted path with space resolves",
			command:     `"${CLAUDE_PLUGIN_ROOT}/hooks/a b.sh"`,
			installed:   []string{"hooks/a b.sh"},
			wantCommand: `"${CLAUDE_PLUGIN_ROOT}/hooks/a b.sh"`,
		},
		{
			name:        "unquoted path stops at delimiter",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh;echo`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh;echo`,
		},
		{
			name:        "double slash resolves",
			command:     `${CLAUDE_PLUGIN_ROOT}//hooks/check.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}//hooks/check.sh`,
		},
		{
			name:        "concat after closing quote unchecked",
			command:     `"${CLAUDE_PLUGIN_ROOT}/hooks/miss"y.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `"${CLAUDE_PLUGIN_ROOT}/hooks/miss"y.sh`,
		},
		{
			name:        "mid-word quote unchecked",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/miss/"x".sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/miss/"x".sh`,
		},
		{
			name:        "concat single-quote after closing quote unchecked",
			command:     `"${CLAUDE_PLUGIN_ROOT}/hooks/miss"'y'.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `"${CLAUDE_PLUGIN_ROOT}/hooks/miss"'y'.sh`,
		},
		{
			name:        "quoted reference before word delimiter checked",
			command:     `"${CLAUDE_PLUGIN_ROOT}/hooks/check.sh" && echo`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `"${CLAUDE_PLUGIN_ROOT}/hooks/check.sh" && echo`,
		},
		{
			name:        "trailing slash resolves directory",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/`,
		},
		{
			name:        "later reference still missing",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh && ${CLAUDE_PLUGIN_ROOT}/hooks/absent.sh`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh && ${CLAUDE_PLUGIN_ROOT}/hooks/absent.sh`,
			wantMissing: true,
		},
		{
			name:        "trailing backslash unchecked no panic",
			command:     `${CLAUDE_PLUGIN_ROOT}/hooks/x\`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/hooks/x\`,
		},
		{
			name:        "dot path names bundle root",
			command:     `${CLAUDE_PLUGIN_ROOT}/.`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/.`,
		},
		{
			name:        "slash-dot path names bundle root",
			command:     `${CLAUDE_PLUGIN_ROOT}/./`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `${CLAUDE_PLUGIN_ROOT}/./`,
		},
		{
			name:        "unterminated quote unchecked",
			command:     `"${CLAUDE_PLUGIN_ROOT}/hooks/miss`,
			installed:   []string{"hooks/check.sh"},
			wantCommand: `"${CLAUDE_PLUGIN_ROOT}/hooks/miss`,
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
		{
			name:        "isolated braced bundle hit",
			command:     `"${CLAUDE_PLUGIN_ROOT}"/hooks/scripts/check.sh`,
			installed:   []string{"hooks/scripts/check.sh"},
			wantCommand: `"${CLAUDE_PLUGIN_ROOT}"/hooks/scripts/check.sh`,
		},
		{
			name:        "isolated unbraced bundle hit",
			command:     `"$CLAUDE_PLUGIN_ROOT"/hooks/scripts/check.sh`,
			installed:   []string{"hooks/scripts/check.sh"},
			wantCommand: `"$CLAUDE_PLUGIN_ROOT"/hooks/scripts/check.sh`,
		},
		{
			name:         "isolated braced legacy rewrites",
			command:      `"${CLAUDE_PLUGIN_ROOT}"/scripts/check.sh`,
			installed:    []string{"hooks/scripts/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `"${CLAUDE_PLUGIN_ROOT}"/hooks/scripts/check.sh`,
			wantLegacy:   true,
		},
		{
			name:         "isolated unbraced legacy rewrites",
			command:      `"$CLAUDE_PLUGIN_ROOT"/scripts/check.sh`,
			installed:    []string{"hooks/scripts/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `"$CLAUDE_PLUGIN_ROOT"/hooks/scripts/check.sh`,
			wantLegacy:   true,
		},
		{
			name:         "isolated braced missing",
			command:      `"${CLAUDE_PLUGIN_ROOT}"/hooks/scripts/absent.sh`,
			installed:    []string{"hooks/scripts/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `"${CLAUDE_PLUGIN_ROOT}"/hooks/scripts/absent.sh`,
			wantMissing:  true,
		},
		{
			name:         "isolated unbraced missing",
			command:      `"$CLAUDE_PLUGIN_ROOT"/hooks/scripts/absent.sh`,
			installed:    []string{"hooks/scripts/check.sh"},
			legacyPrefix: "hooks",
			wantCommand:  `"$CLAUDE_PLUGIN_ROOT"/hooks/scripts/absent.sh`,
			wantMissing:  true,
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
