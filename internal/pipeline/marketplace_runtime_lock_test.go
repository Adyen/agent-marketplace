package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestTransientMarketplaceFailureReplacesStaleOrganizationErrorWithoutDroppingOtherLayers(t *testing.T) {
	required := &config.V4ValidationError{Code: "v4.marketplace-lock-required", Msg: "missing lock"}
	other := errors.New("unrelated user config error")
	cause := errors.New("provisional organization inspection failed")
	ec := config.EffectiveConfig{
		Organization: config.LayerResult{Errors: []error{required}},
		User:         config.LayerResult{Errors: []error{other}},
		Errors:       []error{required, other},
	}
	got := effectiveConfigError(ec, cause)
	if len(got.Organization.Errors) != 1 || !errors.Is(got.Organization.Errors[0], cause) {
		t.Fatalf("organization errors = %v, want the provisional failure", got.Organization.Errors)
	}
	if len(got.Errors) != 2 || !errors.Is(got.Errors[0], cause) || !errors.Is(got.Errors[1], other) {
		t.Fatalf("effective errors = %v, want provisional failure followed by independent error", got.Errors)
	}
	if got.User.Errors[0] != other {
		t.Fatalf("user layer error changed: %v", got.User.Errors)
	}
	got = effectiveConfigError(config.EffectiveConfig{
		User:   config.LayerResult{Errors: []error{other}},
		Errors: []error{other},
	}, cause)
	if len(got.Organization.Errors) != 1 || !errors.Is(got.Organization.Errors[0], cause) || len(got.Errors) != 2 || !errors.Is(got.Errors[1], other) {
		t.Fatalf("unrelated layer errors were lost when organization had no prior error: %#v", got)
	}
	got = effectiveConfigError(config.EffectiveConfig{Organization: config.LayerResult{Errors: []error{required, other}}}, cause)
	if len(got.Errors) != 1 || !errors.Is(got.Errors[0], cause) {
		t.Fatalf("organization errors missing from the effective list produced %v, want only the provisional failure", got.Errors)
	}
}

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
