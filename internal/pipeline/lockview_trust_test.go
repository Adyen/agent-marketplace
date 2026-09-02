//go:build !windows

package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestReadOnlyOrganizationLockViewEnforcesTrustUnit(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readOnlyLayerLock(configPath, lockPath, lockfile.ScopeOrganization, nil, nil)
	if err == nil {
		t.Fatal("read-only organization view accepted missing lock")
	}
	var viewErr *LockViewError
	if !errors.As(err, &viewErr) || viewErr.Code != "lock.trust" {
		t.Fatalf("missing stable lock view trust error: %T %v", err, err)
	}
	var trustErr *config.OrganizationTrustError
	if !errors.As(err, &trustErr) || trustErr.Code != "organization.trust.lock.missing" {
		t.Fatalf("missing wrapped organization trust error: %T %v", err, err)
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
