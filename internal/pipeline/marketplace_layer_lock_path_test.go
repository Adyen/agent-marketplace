package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// A Marketplace edit can target a config whose directory does not exist yet.
// Its lock path must be the canonical name the lock has once both exist, even
// when the home is reached through a symlink.
func TestMarketplaceLayerLockPathForAConfigWhoseDirectoryIsMissing(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "home-link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	for _, config := range []string{
		filepath.Join(home, ".agents", "config.yaml"),
		filepath.Join(link, ".agents", "config.yaml"),
	} {
		got, err := marketplaceLayerLockPath(config)
		if want := filepath.Join(resolvedHome, ".agents", "lock.yaml"); err != nil || got != want {
			t.Fatalf("marketplaceLayerLockPath(%q) = %q, %v; want %q", config, got, err, want)
		}
	}

	dangling := filepath.Join(home, "dangling", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(dangling), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "absent", "config.yaml"), dangling); err != nil {
		t.Fatal(err)
	}
	if got, err := marketplaceLayerLockPath(dangling); err == nil {
		t.Fatalf("marketplaceLayerLockPath accepted a dangling config symlink: %q", got)
	}

	danglingDir := filepath.Join(home, "dangling-dir")
	if err := os.Symlink(filepath.Join(root, "absent"), danglingDir); err != nil {
		t.Fatal(err)
	}
	if got, err := marketplaceLayerLockPath(filepath.Join(danglingDir, "config.yaml")); err == nil {
		t.Fatalf("marketplaceLayerLockPath accepted a config below a dangling directory symlink: %q", got)
	}
}
