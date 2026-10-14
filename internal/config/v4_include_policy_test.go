package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// spec: consumer-config-format/v4-fragments-preserve-include-authority
func TestV4IncludedPolicyDiagnosticsKeepDeclaringFile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source Source
		field  string
	}{
		{"repo logging", RepoCommitted, "logging: {level: debug}"},
		{"repo backup", RepoCommitted, "backup: {enabled: true}"},
		{"repo telemetry", RepoCommitted, "telemetry: {level: basic}"},
		{"local harnesses", RepoPersonal, "harnesses: []"},
		{"local registry allowlist", RepoPersonal, "allowed_registries: []"},
		{"local marketplace allowlist", RepoPersonal, "allowed_marketplaces: []"},
		{"organization materialization", Organization, "materialization: {mode: file}"},
	} {
		for _, nested := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/direct", true: "/nested"}[nested], func(t *testing.T) {
				fixture := newV4OnlyFixture(t)
				root := fixture.paths[tc.source]
				fragment := filepath.Join(filepath.Dir(root), "policy.yaml")
				fragmentBefore := v4OnlyWriteFixture(t, fragment, "# policy declaration\n\n"+tc.field+"\n")
				include := "policy.yaml"
				if nested {
					include = "middle.yaml"
					v4OnlyWriteFixture(t, filepath.Join(filepath.Dir(root), include), "include: policy.yaml\n")
				}
				rootBefore := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\ninclude: "+include+"\n")
				_, err := LoadV4ConfigIfPresent(root, tc.source)
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != "v4.layer-field" || !strings.Contains(err.Error(), fragment) || validation.Line != 3 {
					t.Fatalf("declaring policy context lost: %v, diagnostic=%#v", err, validation)
				}
				intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
				if err := intent.Layers[tc.source].Err(); err == nil || !strings.Contains(err.Error(), fragment) {
					t.Fatalf("intent lost declaring file: %v", err)
				}
				v4OnlyAssertUnchanged(t, root, rootBefore)
				v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
				v4OnlyWriteFixture(t, fragment, "sources: {reg: {directory: /safe}}\nresources: {reg: [skill/control]}\n")
				valid, err := LoadV4ConfigIfPresent(root, tc.source)
				if err != nil || len(valid.Requests) != 1 || valid.Requests[0].Name != "control" {
					t.Fatalf("valid included selection control = %#v, %v", valid, err)
				}
			})
		}
	}
}

func TestV4AuxiliaryValueDiagnosticsKeepDeclaringFile(t *testing.T) {
	for _, field := range []string{"logging: {level: not-a-level}", "allowed_marketplaces: ['[']", "telemetry: {level: not-a-level}", "materialization: {mode: unknown}"} {
		t.Run(field, func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			root := fixture.paths[UserPersonal]
			fragment := filepath.Join(filepath.Dir(root), "policy.yaml")
			before := v4OnlyWriteFixture(t, fragment, "# policy declaration\n\n"+field+"\n")
			v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\n")
			_, err := LoadV4ConfigIfPresent(root, UserPersonal)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), fragment) || validation.Line != 3 {
				t.Fatalf("auxiliary value lost file/line context: %v, diagnostic=%#v", err, validation)
			}
			v4OnlyAssertUnchanged(t, fragment, before)
		})
	}
}
