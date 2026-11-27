package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestProspectiveMarketplacePlaceLocksRefuseMismatchedProfile(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newTargetApprovalFixture(t)
	packageDigest := "sha256:" + strings.Repeat("b", 64)
	userConfig, err := os.ReadFile(fixture.userConfig)
	if err != nil {
		t.Fatal(err)
	}
	withPackage := strings.Replace(string(userConfig),
		"resources:\n  hosted: [tools/target-profile/invented-app]",
		"resources:\n  hosted:\n    include:\n      - tools/target-profile/invented-app\n      - tools/invented-app%2Fmod/sample: {revision: "+packageDigest+"}",
		1,
	)
	if withPackage == string(userConfig) {
		t.Fatal("user fixture lacked the expected profile resource block")
	}
	if err := os.WriteFile(fixture.userConfig, []byte(withPackage), 0o600); err != nil {
		t.Fatal(err)
	}

	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{
		RepoRoot: fixture.repo, LocalConfigDir: fixture.repo, GlobalPath: fixture.userConfig,
	})
	layer := intent.Layers[config.UserPersonal]
	if err := layer.Err(); err != nil {
		t.Fatalf("read user v4 intent: %v", err)
	}
	if len(layer.MarketplaceSources()) != 1 || len(layer.MarketplaceSources()[0].Requests) != 2 {
		t.Fatalf("user profile and scoped package requests = %#v", layer.MarketplaceSources())
	}

	lockPaths, err := config.ResolveConfigLockPaths(fixture.userConfig)
	if err != nil {
		t.Fatal(err)
	}
	originalConfig, err := os.ReadFile(fixture.userConfig)
	if err != nil {
		t.Fatal(err)
	}
	originalLock, err := os.ReadFile(lockPaths.CanonicalLock)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := lockfile.Decode(originalLock)
	if err != nil {
		t.Fatalf("decode deployed user lock: %v", err)
	}
	if len(doc.Marketplaces) != 1 {
		t.Fatalf("deployed Marketplace locks = %#v", doc.Marketplaces)
	}
	binding := targetprofile.Binding{
		PackageContentDigest: packageDigest, DeliveryMappingDigest: "sha256:" + strings.Repeat("c", 64),
		ProfileDigest: fixture.identity.RevisionDigest, ProfileIdentity: fixture.identity,
	}
	packageLock := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "tools-id", Kind: "invented-app/mod", Name: "sample",
		Revision: packageDigest, ContentDigest: lockfile.Digest(packageDigest),
		ArchiveDigest:       lockfile.Digest("sha256:" + strings.Repeat("d", 64)),
		AssessmentDigest:    lockfile.Digest("sha256:" + strings.Repeat("e", 64)),
		HostPackageIdentity: &binding, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	matchingDoc := doc
	matchingDoc.Marketplaces = append([]lockfile.MarketplaceLock(nil), doc.Marketplaces...)
	matchingDoc.Marketplaces[0].Resources = append([]lockfile.MarketplaceResourceLock(nil), doc.Marketplaces[0].Resources...)
	matchingDoc.Marketplaces[0].Resources = append(matchingDoc.Marketplaces[0].Resources, packageLock)
	matchingBytes, err := lockfile.Encode(matchingDoc)
	if err != nil {
		t.Fatalf("encode matching candidate lock: %v", err)
	}
	matching := []dependencytxn.DesiredFile{{Path: lockPaths.CanonicalLock, Bytes: matchingBytes}}
	if _, err := validateProspectiveMarketplacePlaceLocks(context.Background(), intent, matching); err != nil {
		t.Fatalf("matching prospective place lock refused: %v", err)
	}

	for _, tc := range []struct{ name, digest string }{
		{"source-only", binding.ProfileDigest},
		{"source-and-digest", "sha256:" + strings.Repeat("f", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mismatchedBinding := binding
			mismatchedBinding.ProfileIdentity.SourceID = "git:https%3A%2F%2Fexample.org%2Fother@main#profiles%2Finvented-app.yaml"
			mismatchedBinding.ProfileIdentity.RevisionDigest = tc.digest
			mismatchedBinding.ProfileDigest = tc.digest
			if err := mismatchedBinding.ProfileIdentity.Validate(); err != nil {
				t.Fatalf("validate changed full profile identity: %v", err)
			}
			mismatchedDoc, err := lockfile.Decode(matchingBytes)
			if err != nil {
				t.Fatalf("decode matching candidate lock: %v", err)
			}
			resources := mismatchedDoc.Marketplaces[0].Resources
			for i := range resources {
				if resources[i].Kind == packageLock.Kind && resources[i].Name == packageLock.Name {
					resources[i].HostPackageIdentity = &mismatchedBinding
				}
			}
			mismatchedBytes, err := lockfile.Encode(mismatchedDoc)
			if err != nil {
				t.Fatalf("encode mismatched candidate lock: %v", err)
			}
			mismatched := []dependencytxn.DesiredFile{{Path: lockPaths.CanonicalLock, Bytes: mismatchedBytes}}
			if _, err := validateProspectiveMarketplacePlaceLocks(context.Background(), intent, mismatched); err == nil ||
				!strings.Contains(err.Error(), "marketplace.profile.revision-mismatch") ||
				!strings.Contains(err.Error(), "sample") || !strings.Contains(err.Error(), "invented-app") {
				t.Fatalf("mismatched prospective place lock error = %v", err)
			}
		})
	}
	if after, err := os.ReadFile(fixture.userConfig); err != nil || !bytes.Equal(after, originalConfig) {
		t.Fatalf("user config changed during prospective validation: %v", err)
	}
	if after, err := os.ReadFile(lockPaths.CanonicalLock); err != nil || !bytes.Equal(after, originalLock) {
		t.Fatalf("deployed lock changed during prospective validation: %v", err)
	}
}

// spec: target-profile-place/ambiguous-or-unsupported-profiles-refuse-before-writes
func TestSyncRefusesAmbiguousOrUnsupportedPlaceProfiles(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name string
		want []string
	}{
		{
			name: "ambiguous",
			want: []string{
				"marketplace.profile.selection-ambiguous",
				"invented-app",
				"builtin:agent-marketplace",
			},
		},
		{
			name: "unsupported",
			want: []string{"v4.harness", "unknown-app"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTargetApprovalFixture(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.NotFound(w, r)
			}))
			defer server.Close()

			configBytes := bytes.Replace(
				fixture.userConfigBytes, []byte("https://market.example"), []byte(server.URL), 1,
			)
			configBytes = bytes.Replace(
				configBytes,
				[]byte("apiVersion: agent-marketplace/v4\n"),
				[]byte("apiVersion: agent-marketplace/v4\ntargets: [invented-app]\n"),
				1,
			)
			if test.name == "ambiguous" {
				configBytes = bytes.Replace(
					configBytes,
					[]byte("hosted: [tools/target-profile/invented-app]"),
					[]byte("hosted: [tools/target-profile/invented-app, bundled/target-profile/invented-app]"),
					1,
				)
			} else {
				configBytes = bytes.Replace(
					configBytes, []byte("targets: [invented-app]"), []byte("targets: [unknown-app]"), 1,
				)
			}
			if bytes.Equal(configBytes, fixture.userConfigBytes) {
				t.Fatal("test config transformations did not change the authored fixture")
			}

			doc, err := lockfile.Decode(fixture.userLockBytes)
			if err != nil {
				t.Fatalf("decode fixture lock: %v", err)
			}
			if len(doc.Marketplaces) != 1 {
				t.Fatalf("fixture Marketplace locks = %#v, want one", doc.Marketplaces)
			}
			market := doc.Marketplaces[0]
			market.URL = server.URL
			if test.name == "ambiguous" {
				profileIndex := -1
				for i := range market.Resources {
					if market.Resources[i].Kind == "target-profile" &&
						market.Resources[i].Name == fixture.identity.TargetID {
						profileIndex = i
						break
					}
				}
				if profileIndex < 0 {
					t.Fatalf("fixture lock has no external target profile: %#v", market.Resources)
				}
				builtinIdentity := fixture.identity
				builtinIdentity.SourceID = "builtin:agent-marketplace"
				builtinProfile := market.Resources[profileIndex]
				builtinProfile.RegistryName = "bundled"
				builtinProfile.RegistryID = "bundled-id"
				builtinProfile.ProfileIdentity = &builtinIdentity
				market.RegistryBindings = append(
					append([]lockfile.MarketplaceRegistryBinding(nil), market.RegistryBindings...),
					lockfile.MarketplaceRegistryBinding{Name: "bundled", RegistryID: "bundled-id"},
				)
				market.Resources = append(
					append([]lockfile.MarketplaceResourceLock(nil), market.Resources...),
					builtinProfile,
				)
			}
			doc.Marketplaces[0] = market
			lockBytes, err := lockfile.Encode(doc)
			if err != nil {
				t.Fatalf("encode fixture lock: %v", err)
			}
			if err := os.WriteFile(fixture.userConfig, configBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.userLock, lockBytes, 0o600); err != nil {
				t.Fatal(err)
			}

			configBefore := snapshotTargetStateTree(t, fixture.userConfig)
			lockBefore := snapshotTargetStateTree(t, fixture.userLock)
			targetBefore := snapshotTargetStateTree(t, fixture.targetRoot)
			stateBefore := snapshotTargetStateTree(t, fixture.stateRoot)
			for _, args := range [][]string{{"sync"}, {"sync", "--offline"}} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					output, err := executeCommand(args...)
					diagnostic := output
					if err != nil {
						diagnostic += "\n" + err.Error()
					}
					if err == nil {
						t.Fatalf("%s accepted %s profile selection:\n%s", strings.Join(args, " "), test.name, output)
					}
					for _, want := range test.want {
						if !strings.Contains(diagnostic, want) {
							t.Fatalf("%s %s diagnostic = %q, want %q", strings.Join(args, " "), test.name, diagnostic, want)
						}
					}
					if test.name == "ambiguous" && !strings.Contains(diagnostic, fixture.identity.SourceID) {
						t.Fatalf("ambiguous profile diagnostic omitted external source %q: %s", fixture.identity.SourceID, diagnostic)
					}
					if got := requests.Load(); got != 0 {
						t.Fatalf("%s %s made %d hosted requests before refusal", strings.Join(args, " "), test.name, got)
					}
					assertTargetStateTreeUnchanged(t, configBefore, fixture.userConfig)
					assertTargetStateTreeUnchanged(t, lockBefore, fixture.userLock)
					assertTargetStateTreeUnchanged(t, targetBefore, fixture.targetRoot)
					assertTargetStateTreeUnchanged(t, stateBefore, fixture.stateRoot)
				})
			}
		})
	}
}
