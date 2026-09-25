package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestTransientMarketplaceReadAssertionsRequireLockAbsence(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "lock.yaml")
	if err := os.WriteFile(lockPath, []byte("deployed"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := transientMarketplaceReadAssertions(config.Intent{}, lockPath)
	if err == nil || !strings.Contains(err.Error(), "must remain absent") {
		t.Fatalf("error = %v, want deployed-lock absence failure", err)
	}
}

func TestLoadEffectiveOnlineReadsIntentOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	fixture := newMarketplaceUpdateFixture(t, false)

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	if err := os.WriteFile(orgPath, []byte("apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+fixture.server.URL+"}\nresources:\n  hosted: {include: [renamed-source/skill/review]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	calls := 0
	delegate := readIntent
	readIntent = func(ctx context.Context, opts config.LoadEffectiveOptions) config.Intent {
		calls++
		return delegate(ctx, opts)
	}
	t.Cleanup(func() { readIntent = delegate })

	ec := LoadEffective(context.Background(), ConfigLoadOnline, config.LoadEffectiveOptions{
		RepoRoot:         root,
		LocalConfigDir:   root,
		OrganizationPath: orgPath,
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(root, ".agents", "config.yaml"),
		LocalPath:        filepath.Join(root, ".agents", "config.local.yaml"),
	}, nil)
	if len(ec.Errors) > 0 {
		t.Fatalf("online load errors = %v", ec.Errors)
	}
	if calls != 1 {
		t.Fatalf("ReadIntent calls = %d, want exactly one per online load", calls)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("organization lock was created: %v", err)
	}
}
