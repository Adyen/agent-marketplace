package hookpolicy

import "testing"

type wantPluginRootReference struct {
	token   string
	quote   byte
	bare    bool
	path    string
	dynamic bool
}

func TestPluginRootReferences(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []wantPluginRootReference
	}{
		{
			name:    "braced isolated token",
			command: `"${CLAUDE_PLUGIN_ROOT}"/scripts/check.sh`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", quote: '"', path: "scripts/check.sh"}},
		},
		{
			name:    "unbraced isolated token",
			command: `"$CLAUDE_PLUGIN_ROOT"/scripts/check.sh`,
			want:    []wantPluginRootReference{{token: "$CLAUDE_PLUGIN_ROOT", quote: '"', path: "scripts/check.sh"}},
		},
		{
			name:    "whole word quoted",
			command: `"${CLAUDE_PLUGIN_ROOT}/hooks/x.sh"`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", quote: '"', path: "hooks/x.sh"}},
		},
		{
			name:    "unquoted",
			command: `${CLAUDE_PLUGIN_ROOT}/hooks/x.sh`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", path: "hooks/x.sh"}},
		},
		{
			name:    "bare quoted token",
			command: `cd "${CLAUDE_PLUGIN_ROOT}"`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", quote: '"', bare: true}},
		},
		{
			name:    "single quoted",
			command: `echo '${CLAUDE_PLUGIN_ROOT}/x.sh'`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", quote: '\'', path: "x.sh", dynamic: true}},
		},
		{
			name:    "expansion in path",
			command: `${CLAUDE_PLUGIN_ROOT}/hooks/$SCRIPT`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", path: "hooks/$SCRIPT", dynamic: true}},
		},
		{
			name:    "glob in path",
			command: `${CLAUDE_PLUGIN_ROOT}/hooks/*.sh`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", path: "hooks/*.sh", dynamic: true}},
		},
		{
			name:    "reference inside command substitution keeps double quotes",
			command: `"$(cat "${CLAUDE_PLUGIN_ROOT}/hooks/x.sh")"`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", quote: '"', path: "hooks/x.sh"}},
		},
		{
			name:    "option assignment",
			command: `run --config=${CLAUDE_PLUGIN_ROOT}/hooks/c.json`,
			want:    []wantPluginRootReference{{token: "${CLAUDE_PLUGIN_ROOT}", path: "hooks/c.json"}},
		},
		{
			name:    "droid spelling",
			command: `"${DROID_PLUGIN_ROOT}/hooks/x.sh"`,
			want:    []wantPluginRootReference{{token: "${DROID_PLUGIN_ROOT}", quote: '"', path: "hooks/x.sh"}},
		},
		{
			name:    "other variables are not plugin roots",
			command: `"$HOME"/x.sh`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			refs, err := PluginRootReferences(tc.command)
			if err != nil {
				t.Fatalf("PluginRootReferences: %v", err)
			}
			if len(refs) != len(tc.want) {
				t.Fatalf("got %d references %+v, want %d", len(refs), refs, len(tc.want))
			}
			for i, want := range tc.want {
				ref := refs[i]
				if got := tc.command[ref.Start:ref.End]; got != want.token {
					t.Errorf("token = %q, want %q", got, want.token)
				}
				if ref.Quote != want.quote {
					t.Errorf("quote = %q, want %q", ref.Quote, want.quote)
				}
				if want.bare {
					if ref.PathStart != -1 {
						t.Errorf("bare token PathStart = %d, want -1", ref.PathStart)
					}
				} else if got := tc.command[ref.PathStart:ref.PathEnd]; got != want.path {
					t.Errorf("path = %q, want %q", got, want.path)
				}
				if ref.Dynamic != want.dynamic {
					t.Errorf("dynamic = %t, want %t", ref.Dynamic, want.dynamic)
				}
			}
		})
	}
}
