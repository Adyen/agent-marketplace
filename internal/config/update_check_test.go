package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeUpdateCheckConfig(t *testing.T, path, enabled string) {
	t.Helper()
	if enabled == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\nupdate_check: {enabled: "+enabled+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// spec: upgrade-notice/organization-configuration-disables-the-check
// spec: upgrade-notice/user-configuration-overrides-the-organization-setting
func TestUpdateCheckEnabledLayers(t *testing.T) {
	for _, tc := range []struct {
		name, org, user string
		want            bool
	}{
		{name: "no configuration", want: true},
		{name: "user disables", user: "false", want: false},
		{name: "organization disables", org: "false", want: false},
		{name: "user overrides organization", org: "false", user: "true", want: true},
		{name: "user disables over organization enabled", org: "true", user: "false", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			home := filepath.Join(dir, "home")
			t.Setenv("HOME", home)
			orgPath := filepath.Join(dir, "organization.yaml")
			SetTestOrganizationConfigPath(t, orgPath)
			writeUpdateCheckConfig(t, orgPath, tc.org)
			writeUpdateCheckConfig(t, filepath.Join(home, ".agents", "config.yaml"), tc.user)

			got, err := UpdateCheckEnabled()
			if err != nil {
				t.Fatalf("UpdateCheckEnabled() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("UpdateCheckEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
