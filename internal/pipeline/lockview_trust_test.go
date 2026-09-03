//go:build !windows

package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestReadOnlyOrganizationLockViewAcceptsMissingLock(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := readOnlyLayerLock(configPath, lockPath, lockfile.ScopeOrganization, nil, nil)
	if err != nil || doc.APIVersion != "" {
		t.Fatalf("missing optional organization lock returned doc=%#v err=%v", doc, err)
	}
}

func TestReadOnlyOrganizationLockViewAcceptsTrustedFixture(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")
	for path, data := range map[string]string{
		configPath: "apiVersion: agent-marketplace/v2\nregistries: []\n",
		lockPath:   "apiVersion: agent-marketplace-lock/v1\nscope: organization\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := readOnlyLayerLock(configPath, lockPath, lockfile.ScopeOrganization, nil, nil)
	if err != nil || doc.Scope != lockfile.ScopeOrganization {
		t.Fatalf("trusted read-only organization view failed: scope=%s err=%v", doc.Scope, err)
	}
}
