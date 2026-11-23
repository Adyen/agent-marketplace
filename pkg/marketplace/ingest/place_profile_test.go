package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
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
