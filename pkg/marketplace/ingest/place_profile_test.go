package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const checkoutProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: xdg-config, path: invented-app}}, allowed_bases: [home, xdg-config]}}
host: {executable: invented-app-bin, version_probe: {argv: [--version], pattern: '(\d+\.\d+\.\d+)'}, verified: [{range: ">=1.0.0 <2.0.0", date: 2026-11-12, evidence: fixture}]}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}
`

const (
	checkoutProfileSourceID = "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Finvented-app.yaml"
	checkoutProfileOrigin   = "https://git.example.com/tools.git"
)

func TestResolveCheckoutAssessmentProfile(t *testing.T) {
	profileBytes := []byte(checkoutProfileYAML)
	identity := checkoutAssessmentProfileIdentity(profileBytes)
	checkout, root := openAssessmentProfileCheckout(t, profileBytes)

	got, err := resolveCheckoutAssessmentProfile(checkout, checkoutProfileOrigin, "main", identity)
	if err != nil {
		t.Fatalf("resolveCheckoutAssessmentProfile() error = %v", err)
	}
	if !bytes.Equal(got, profileBytes) {
		t.Fatal("resolved profile bytes differ from the exact profile path")
	}
	unrelated, err := os.ReadFile(filepath.Join(root, "unrelated", "invented-app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, unrelated) {
		t.Fatal("same-basename unrelated profile did not provide a distinct positive control")
	}

	changedDigest := identity
	changedDigest.RevisionDigest = "sha256:" + strings.Repeat("0", 64)
	missingPath := identity
	missingPath.SourceID = "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Fmissing.yaml"
	builtin := identity
	builtin.SourceID = "builtin:agent-marketplace"
	tests := []struct {
		name, origin, ref, wantCode string
		identity                    targetprofile.Identity
		makeSymlink                 bool
	}{
		{name: "wrong origin", origin: "https://git.example.com/other.git", ref: "main", identity: identity, wantCode: "marketplace.profile.unavailable"},
		{name: "wrong ref", origin: checkoutProfileOrigin, ref: "release", identity: identity, wantCode: "marketplace.profile.unavailable"},
		{name: "changed profile digest", origin: checkoutProfileOrigin, ref: "main", identity: changedDigest, wantCode: "marketplace.profile.digest-mismatch"},
		{name: "missing path", origin: checkoutProfileOrigin, ref: "main", identity: missingPath, wantCode: "marketplace.profile.unavailable"},
		{name: "symlink path", origin: checkoutProfileOrigin, ref: "main", identity: identity, makeSymlink: true, wantCode: "marketplace.profile.unavailable"},
		{name: "builtin identity", origin: checkoutProfileOrigin, ref: "main", identity: builtin, wantCode: "marketplace.profile.unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current, currentRoot := openAssessmentProfileCheckout(t, profileBytes)
			if tt.makeSymlink {
				file := filepath.Join(currentRoot, "profiles", "invented-app.yaml")
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "invented-app.yaml")
				writeAssessmentProfileFile(t, target, profileBytes)
				if err := os.Symlink(target, file); err != nil {
					t.Fatal(err)
				}
			}
			origin := tt.origin
			if origin == "" {
				origin = checkoutProfileOrigin
			}
			_, err := resolveCheckoutAssessmentProfile(current, origin, tt.ref, tt.identity)
			if err == nil || !strings.Contains(err.Error(), tt.wantCode) {
				t.Fatalf("resolveCheckoutAssessmentProfile() error = %v, want %s refusal", err, tt.wantCode)
			}
		})
	}
}

func TestPlaceCheckoutPaths(t *testing.T) {
	profileBytes := []byte(checkoutProfileYAML)
	identity := checkoutAssessmentProfileIdentity(profileBytes)
	cfg := config.Config{HostPackageAssessmentProfiles: []config.HostPackageAssessmentProfile{{
		RegistryID: "src-test", Kind: "invented-app/mod", Name: "sample", ProfileIdentity: identity,
	}}}
	registry := store.Registry{
		ID: "src-test", Ref: "main", Origins: []string{checkoutProfileOrigin}, Acquisition: store.AcquisitionMirror,
	}
	got, err := PlaceCheckoutPaths(cfg, registry)
	if err != nil {
		t.Fatalf("PlaceCheckoutPaths: %v", err)
	}
	want := []string{"profiles/invented-app.yaml", "packages/invented-app/mod/sample"}
	if !slices.Equal(got, want) {
		t.Fatalf("PlaceCheckoutPaths() = %q, want %q", got, want)
	}

	empty, err := PlaceCheckoutPaths(config.Config{}, registry)
	if err != nil || len(empty) != 0 {
		t.Fatalf("PlaceCheckoutPaths without selections = %q, %v; want empty", empty, err)
	}
	t.Run("Git metadata cannot be selected", func(t *testing.T) {
		unsafe := cfg.HostPackageAssessmentProfiles[0]
		unsafe.ProfileIdentity.SourceID = strings.Replace(identity.SourceID, "#profiles", "#.Git", 1)
		unsafeCfg := config.Config{HostPackageAssessmentProfiles: []config.HostPackageAssessmentProfile{unsafe}}
		if _, err := PlaceCheckoutPaths(unsafeCfg, registry); err == nil || !strings.Contains(err.Error(), "marketplace.profile.unavailable") {
			t.Fatalf("PlaceCheckoutPaths for Git metadata = %v, want unavailable", err)
		}
	})
	badRegistry := registry
	badRegistry.Origins = []string{"https://git.example.com/other.git"}
	if _, err := PlaceCheckoutPaths(cfg, badRegistry); err == nil || !strings.Contains(err.Error(), "marketplace.profile.unavailable") {
		t.Fatalf("PlaceCheckoutPaths with different origin error = %v, want unavailable", err)
	}
	badRegistry = registry
	badRegistry.Ref = "other"
	if _, err := PlaceCheckoutPaths(cfg, badRegistry); err == nil || !strings.Contains(err.Error(), "marketplace.profile.unavailable") {
		t.Fatalf("PlaceCheckoutPaths with different ref error = %v, want unavailable", err)
	}
	badRegistry = registry
	badRegistry.Acquisition = store.AcquisitionIndexOnly
	if _, err := PlaceCheckoutPaths(cfg, badRegistry); err == nil || !strings.Contains(err.Error(), "marketplace.package.acquisition-unsupported") {
		t.Fatalf("PlaceCheckoutPaths in index-only mode error = %v, want acquisition refusal", err)
	}
}

func checkoutAssessmentProfileIdentity(data []byte) targetprofile.Identity {
	digest := sha256.Sum256(data)
	return targetprofile.Identity{
		TargetID: "invented-app", SourceID: checkoutProfileSourceID,
		RevisionDigest: "sha256:" + hex.EncodeToString(digest[:]),
	}
}

func openAssessmentProfileCheckout(t *testing.T, profile []byte) (*fsboundary.Dir, string) {
	t.Helper()
	root := t.TempDir()
	writeAssessmentProfileFile(t, filepath.Join(root, "profiles", "invented-app.yaml"), profile)
	writeAssessmentProfileFile(t, filepath.Join(root, "unrelated", "invented-app.yaml"), []byte("different profile bytes"))
	checkout, err := fsboundary.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := checkout.Close(); err != nil {
			t.Errorf("close checkout: %v", err)
		}
	})
	return checkout, root
}

func writeAssessmentProfileFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
