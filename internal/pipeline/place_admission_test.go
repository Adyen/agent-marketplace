package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

const placeAdmissionProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: home, path: .config/invented-app}}, allowed_bases: [home]}}
kinds: {mod: {deliver: place, into: [mods/]}}
`

type placeAdmissionFixture struct {
	raw      []byte
	identity targetprofile.Identity
	binding  targetprofile.ApprovalBinding
	config   *config.Config
	resolver *marketplace.Resolver
	env      targetprofile.ApprovalEnvironment
	root     string
	state    string
	requests *placeAdmissionHTTPProbe
}

func newPlaceAdmissionFixture(t *testing.T) placeAdmissionFixture {
	t.Helper()
	return newPlaceAdmissionFixtureWithProfile(t, []byte(placeAdmissionProfileYAML))
}

func newPlaceAdmissionFixtureWithProfile(t *testing.T, raw []byte) placeAdmissionFixture {
	t.Helper()
	digest := sha256.Sum256(raw)
	identity := targetprofile.Identity{
		TargetID: "invented-app", SourceID: "builtin:agent-marketplace",
		RevisionDigest: "sha256:" + hex.EncodeToString(digest[:]),
	}
	profile, err := targetprofile.Parse(raw, identity)
	if err != nil {
		t.Fatalf("parse test profile: %v", err)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test home: %v", err)
	}
	root := filepath.Join(home, ".config", "invented-app")
	state := filepath.Join(home, ".local", "state", "agent-marketplace")
	for _, path := range []string{root, state} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("create test root %s: %v", path, err)
		}
	}
	t.Setenv("INVENTED_APP_HOME", root)
	override := root
	env := targetprofile.ApprovalEnvironment{
		Platform: "linux", Home: home, CanonicalStateRoot: state, Override: &override,
	}
	binding, err := targetprofile.ResolveApprovalBinding(profile, identity, env)
	if err != nil {
		t.Fatalf("resolve test binding: %v", err)
	}
	files := []targetprofile.PackageFile{{
		SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
		Mode: 0o644, Bytes: []byte("fixture package\n"),
	}}
	hostBinding, err := targetprofile.BuildBinding(identity, files)
	if err != nil {
		t.Fatalf("build package binding: %v", err)
	}
	packagePin := config.MarketplaceResourcePin{
		RegistryID: "tools-id", RegistrySlug: "tools", Kind: "invented-app/mod", Name: "sample",
		Revision: hostBinding.PackageContentDigest, Digest: hostBinding.PackageContentDigest,
		Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
		LockArchiveDigest:       "sha256:" + strings.Repeat("a", 64),
		LockAssessmentDigest:    "sha256:" + strings.Repeat("b", 64),
		LockHostPackageIdentity: hostBinding,
	}
	profilePin := config.MarketplaceResourcePin{
		RegistryID: "tools-id", RegistrySlug: "tools", Kind: "target-profile", Name: identity.TargetID,
		Revision: identity.RevisionDigest, Digest: identity.RevisionDigest,
		Acquisition: config.MarketplaceAcquisitionMirror, Direct: true, LockProfileIdentity: identity,
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example", InstanceID: "market-id", Source: config.UserPersonal,
		Resources: []config.MarketplaceResourcePin{profilePin, packagePin},
	}}}
	cacheRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(cacheRoot, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := marketplace.NewCache(filepath.Join(cacheRoot, "empty"))
	probe := &placeAdmissionHTTPProbe{}
	marketplaceResolver := &marketplace.Resolver{
		Cache: cache, HTTPClient: &http.Client{Transport: probe},
	}
	return placeAdmissionFixture{
		raw: raw, identity: identity, binding: binding, config: cfg, resolver: marketplaceResolver,
		env: env, root: root, state: state, requests: probe,
	}
}

func TestPreparePlaceAdmissionRequiresReceiptBeforeResolving(t *testing.T) {
	fixture := newPlaceAdmissionFixture(t)
	ctx := context.Background()
	var reads atomic.Int32
	readProfile := func(_ context.Context, selected targetprofile.Identity, mode resolver.MarketplaceResolveMode) ([]byte, error) {
		reads.Add(1)
		if selected != fixture.identity || mode != resolver.MarketplaceResolveModeCachedNoFetch {
			t.Fatalf("profile reader arguments = identity %#v, mode %v", selected, mode)
		}
		return append([]byte(nil), fixture.raw...), nil
	}
	mode := resolver.MarketplaceResolveModeCachedNoFetch

	disabled, err := PreparePlaceAdmission(ctx, fixture.config, nil, fixture.env, readProfile, fixture.resolver, mode)
	if err != nil || len(disabled.Pending) != 0 || len(disabled.Approvals) != 0 || len(disabled.Packages) != 0 {
		t.Fatalf("disabled admission = %#v, %v", disabled, err)
	}
	if reads.Load() != 0 {
		t.Fatalf("disabled admission called profile reader %d times", reads.Load())
	}

	pending, err := PreparePlaceAdmission(ctx, fixture.config, []string{fixture.identity.TargetID}, fixture.env, readProfile, fixture.resolver, mode)
	if err != nil || len(pending.Pending) != 1 || pending.Pending[0] != fixture.identity ||
		len(pending.Approvals) != 0 || len(pending.Packages) != 0 {
		t.Fatalf("missing-receipt admission = %#v, %v", pending, err)
	}
	if reads.Load() != 1 {
		t.Fatalf("missing-receipt profile reads = %d, want 1", reads.Load())
	}
	if fixture.requests.requests.Load() != 0 {
		t.Fatalf("missing receipt issued %d HTTP requests", fixture.requests.requests.Load())
	}
	if entries, err := os.ReadDir(fixture.state); err != nil || len(entries) != 0 {
		t.Fatalf("missing-receipt admission wrote state: entries=%v err=%v", entries, err)
	}
	if entries, err := os.ReadDir(fixture.root); err != nil || len(entries) != 0 {
		t.Fatalf("missing-receipt admission wrote target root: entries=%v err=%v", entries, err)
	}

	changed := bytes.Replace(fixture.raw, []byte("label: Invented App"), []byte("label: Changed App"), 1)
	mismatchedReader := func(context.Context, targetprofile.Identity, resolver.MarketplaceResolveMode) ([]byte, error) {
		return changed, nil
	}
	mismatched, err := PreparePlaceAdmission(ctx, fixture.config, []string{fixture.identity.TargetID}, fixture.env, mismatchedReader, fixture.resolver, mode)
	if err == nil || len(mismatched.Pending) != 0 || !strings.Contains(err.Error(), "marketplace.profile.digest-mismatch") {
		t.Fatalf("mismatched profile admission = %#v, %v", mismatched, err)
	}
	if fixture.requests.requests.Load() != 0 {
		t.Fatalf("mismatched profile issued %d HTTP requests", fixture.requests.requests.Load())
	}

	if err := targetprofile.RecordApproval(fixture.binding); err != nil {
		t.Fatalf("record test approval: %v", err)
	}
	admitted, err := PreparePlaceAdmission(ctx, fixture.config, []string{fixture.identity.TargetID}, fixture.env, readProfile, fixture.resolver, mode)
	if err == nil || len(admitted.Pending) != 0 || !strings.Contains(err.Error(), "marketplace.cache.place-metadata-missing") {
		t.Fatalf("approved admission = %#v, %v; want exact offline cache miss", admitted, err)
	}
	if reads.Load() != 2 || fixture.requests.requests.Load() != 0 {
		t.Fatalf("approved admission reads=%d HTTP=%d", reads.Load(), fixture.requests.requests.Load())
	}

	entries, err := os.ReadDir(fixture.state)
	if err != nil || len(entries) != 1 {
		t.Fatalf("receipt entries = %v, %v", entries, err)
	}
	receiptPath := filepath.Join(fixture.state, entries[0].Name())
	validReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(`{"schema_version":1,`), validReceipt[1:]...)
	if err := os.WriteFile(receiptPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	refused, err := PreparePlaceAdmission(ctx, fixture.config, []string{fixture.identity.TargetID}, fixture.env, readProfile, fixture.resolver, mode)
	if err == nil || len(refused.Pending) != 0 || !strings.Contains(err.Error(), "target.approval.receipt-invalid") {
		t.Fatalf("tampered receipt admission = %#v, %v", refused, err)
	}
	if fixture.requests.requests.Load() != 0 {
		t.Fatalf("tampered receipt issued %d HTTP requests", fixture.requests.requests.Load())
	}
}

type placeAdmissionHTTPProbe struct{ requests atomic.Int32 }

func (p *placeAdmissionHTTPProbe) RoundTrip(*http.Request) (*http.Response, error) {
	p.requests.Add(1)
	return nil, errors.New("unexpected network request")
}
