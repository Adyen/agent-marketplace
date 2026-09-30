package config

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// The organization layer is the one Configuration Layer that an isolated HOME
// does not isolate, because it lives at a fixed absolute path. These tests pin
// the resolution that keeps a test binary off that file, so a package does not
// have to remember to ask for isolation to get it.

func TestUnsetOrganizationConfigPathDoesNotResolveToTheRealFileInTests(t *testing.T) {
	// No override is set here, which is exactly the state a new test starts in.
	got := organizationConfigPath("")

	if got == defaultOrganizationConfigPath {
		t.Fatalf("organization config path resolved to the real managed file %q; a test would read whatever this machine has installed, inherit its registries, and fetch them over the network", got)
	}
	if _, err := os.Stat(got); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("organization config path %q must not exist, stat error = %v", got, err)
	}
}

func TestUnsetOrganizationConfigPathLoadsAnAbsentOrganizationLayer(t *testing.T) {
	dir := t.TempDir()

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		RepoRoot:   dir,
		GlobalPath: dir + "/missing-global.yaml",
		RepoPath:   dir + "/missing-repo.yaml",
		LocalPath:  dir + "/missing-local.yaml",
	})

	if ec.Organization.Config != nil {
		t.Fatalf("organization layer loaded a config with no override set: %+v", ec.Organization.Config)
	}
	if ec.Managed {
		t.Fatal("effective config reported a managed installation with no override set")
	}
	if strings.HasPrefix(ec.Diagnostics.Path, "/etc/") {
		t.Fatalf("diagnostics reported the real managed path %q", ec.Diagnostics.Path)
	}
	if ec.Merged != nil && len(ec.Merged.Registries) > 0 {
		t.Fatalf("merged config inherited %d registries from outside the test", len(ec.Merged.Registries))
	}
}

// An explicit choice still wins, so isolation does not cost expressiveness.
func TestExplicitOrganizationConfigPathStillWinsOverTheTestDefault(t *testing.T) {
	explicit := t.TempDir() + "/org.yaml"

	if got := organizationConfigPath(explicit); got != explicit {
		t.Fatalf("explicit option path = %q, want %q", got, explicit)
	}

	SetTestOrganizationConfigPath(t, explicit)
	if got := organizationConfigPath(""); got != explicit {
		t.Fatalf("package override path = %q, want %q", got, explicit)
	}
}
