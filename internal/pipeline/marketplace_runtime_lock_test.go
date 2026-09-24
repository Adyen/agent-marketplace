package pipeline

import (
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

	_, err := transientMarketplaceReadAssertions(config.EffectiveConfig{}, lockPath)
	if err == nil || !strings.Contains(err.Error(), "must remain absent") {
		t.Fatalf("error = %v, want deployed-lock absence failure", err)
	}
}

func TestTransientOrganizationMarketplaceInputsDistinguishEachAcquisitionInput(t *testing.T) {
	base := config.EffectiveConfig{
		TargetHarnesses:     []string{"factory"},
		Allowed:             []string{"^https://git\\.example\\.com/"},
		AllowedMarketplaces: []string{"^https://mp\\.example\\.com$"},
	}
	variants := map[string]func(*config.EffectiveConfig){
		"target harnesses":            func(ec *config.EffectiveConfig) { ec.TargetHarnesses = []string{"factory", "claude"} },
		"registry allowlist":          func(ec *config.EffectiveConfig) { ec.Allowed = []string{"^https://other\\.example\\.com/"} },
		"registry allowlist empty":    func(ec *config.EffectiveConfig) { ec.Allowed = []string{} },
		"registry allowlist nil":      func(ec *config.EffectiveConfig) { ec.Allowed = nil },
		"marketplace allowlist":       func(ec *config.EffectiveConfig) { ec.AllowedMarketplaces = []string{"^https://other\\.example\\.com$"} },
		"marketplace allowlist empty": func(ec *config.EffectiveConfig) { ec.AllowedMarketplaces = []string{} },
		"marketplace allowlist nil":   func(ec *config.EffectiveConfig) { ec.AllowedMarketplaces = nil },
	}
	want := transientOrganizationMarketplaceInputs(base)
	seen := map[string]string{want: "base"}
	for name, mutate := range variants {
		ec := base
		mutate(&ec)
		got := transientOrganizationMarketplaceInputs(ec)
		if previous, duplicate := seen[got]; duplicate {
			t.Errorf("%s fingerprint %q collides with %s", name, got, previous)
		}
		seen[got] = name
	}
	nilAllowed, emptyAllowed := base, base
	nilAllowed.Allowed, emptyAllowed.Allowed = nil, []string{}
	nilAllowed.AllowedMarketplaces, emptyAllowed.AllowedMarketplaces = nil, []string{}
	if transientOrganizationMarketplaceInputs(nilAllowed) == transientOrganizationMarketplaceInputs(emptyAllowed) {
		t.Error("nil allowlists (allow all) and empty allowlists (deny all) share a fingerprint")
	}
}

func TestTransientMarketplaceReadAssertionsCoverOnlyOrganizationLayer(t *testing.T) {
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	userPath := filepath.Join(t.TempDir(), "user.yaml")
	repoPath := filepath.Join(t.TempDir(), "repo.yaml")
	lockPath := filepath.Join(t.TempDir(), "lock.yaml")
	ec := config.EffectiveConfig{
		Organization: config.LayerResult{Reads: []config.ConfigRead{{
			Path:          orgPath,
			RequestedPath: orgPath,
			Digest:        "org-digest",
		}}},
		User: config.LayerResult{Reads: []config.ConfigRead{{
			Path:          userPath,
			RequestedPath: userPath,
			Digest:        "user-digest",
		}}},
		Repo: config.LayerResult{Reads: []config.ConfigRead{{
			Path:          repoPath,
			RequestedPath: repoPath,
			Digest:        "repo-digest",
		}}},
	}

	reads, err := transientMarketplaceReadAssertions(ec, lockPath)
	if err != nil {
		t.Fatalf("transientMarketplaceReadAssertions: %v", err)
	}
	asserted := make(map[string]bool, len(reads))
	for _, read := range reads {
		asserted[read.RequestedPath] = true
	}
	if !asserted[orgPath] {
		t.Errorf("assertions do not contain organization read %q", orgPath)
	}
	if !asserted[lockPath] {
		t.Errorf("assertions do not contain lock absence %q", lockPath)
	}
	if asserted[userPath] {
		t.Errorf("assertions unexpectedly contain user read %q", userPath)
	}
	if asserted[repoPath] {
		t.Errorf("assertions unexpectedly contain repo read %q", repoPath)
	}
}
