package mapping

import (
	"testing"
)

func TestDevinModel(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		wantOK bool
	}{
		{"sonnet alias", "sonnet", "sonnet", true},
		{"opus alias", "opus", "opus", true},
		{"haiku alias", "haiku", "haiku", true},
		{"gemini flash family maps to the documented gemini alias", "gemini-3-flash-preview", "gemini", true},
		{"gemini pro family has no documented alias", "gemini-3-preview", "", false},
		{"versioned sonnet is unmappable, never passthrough", "claude-sonnet-4-5-20250929", "", false},
		{"versioned opus is unmappable, never passthrough", "claude-opus-4-20250514", "", false},
		{"versioned haiku is unmappable, never passthrough", "claude-haiku-3-5-20241022", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Model(tt.source, TargetDevin)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Model(%q, Devin) = (%q, %v), want (%q, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDevinModelWithOverride(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		overrides map[string]string
		want      string
		wantOK    bool
	}{
		{"curated value overridden", "sonnet", map[string]string{"sonnet": "custom-model"}, "custom-model", true},
		{"unmappable value rescued by override", "gemini-3-preview", map[string]string{"gemini-3-preview": "custom"}, "custom", true},
		{"no override falls back to table", "sonnet", map[string]string{"other": "x"}, "sonnet", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ModelWithOverride(tt.source, TargetDevin, tt.overrides)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("ModelWithOverride(%q, Devin, %v) = (%q, %v), want (%q, %v)", tt.source, tt.overrides, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDevinMcpInterpolation(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		wantOK bool
	}{
		{"inline values use env interpolation", "inline", "${env:VAR}", true},
		{"canonical env token", "${env:VAR}", "${env:VAR}", true},
		{"file interpolation is unmappable", "${file:PATH}", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := McpInterpolationSupported(tt.source, TargetDevin)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("McpInterpolationSupported(%q, Devin) = (%q, %v), want (%q, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
			if plain := McpInterpolation(tt.source, TargetDevin); plain != tt.want {
				t.Errorf("McpInterpolation(%q, Devin) = %q, want %q", tt.source, plain, tt.want)
			}
		})
	}
}

func TestDevinEnvTokenRequiresCallerSuppliedPluginRoot(t *testing.T) {
	value, literal := EnvToken("${CLAUDE_PLUGIN_ROOT}", TargetDevin)
	if value != "" || !literal {
		t.Errorf("EnvToken(${CLAUDE_PLUGIN_ROOT}, Devin) = (%q, %v), want (\"\", true)", value, literal)
	}
}
