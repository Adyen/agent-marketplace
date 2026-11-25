package main

import (
	"bytes"
	"context"
	"os"
	"strings"
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

	mismatchedBinding := binding
	mismatchedBinding.ProfileIdentity.SourceID = "git:https%3A%2F%2Fexample.org%2Fother@main#profiles%2Finvented-app.yaml"
	mismatchedBinding.ProfileIdentity.RevisionDigest = "sha256:" + strings.Repeat("f", 64)
	mismatchedBinding.ProfileDigest = mismatchedBinding.ProfileIdentity.RevisionDigest
	if err := mismatchedBinding.ProfileIdentity.Validate(); err != nil {
		t.Fatalf("validate changed full profile identity: %v", err)
	}
	mismatchedPackageLock := packageLock
	mismatchedPackageLock.HostPackageIdentity = &mismatchedBinding
	mismatchedDoc := matchingDoc
	mismatchedDoc.Marketplaces = append([]lockfile.MarketplaceLock(nil), matchingDoc.Marketplaces...)
	mismatchedDoc.Marketplaces[0].Resources = append([]lockfile.MarketplaceResourceLock(nil), matchingDoc.Marketplaces[0].Resources...)
	mismatchedDoc.Marketplaces[0].Resources[len(mismatchedDoc.Marketplaces[0].Resources)-1] = mismatchedPackageLock
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
	if after, err := os.ReadFile(fixture.userConfig); err != nil || !bytes.Equal(after, originalConfig) {
		t.Fatalf("user config changed during prospective validation: %v", err)
	}
	if after, err := os.ReadFile(lockPaths.CanonicalLock); err != nil || !bytes.Equal(after, originalLock) {
		t.Fatalf("deployed lock changed during prospective validation: %v", err)
	}
}
