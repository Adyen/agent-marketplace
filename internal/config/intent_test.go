package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadEffectiveCanceledContextDoesNotBind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ec := LoadEffective(ctx, LoadEffectiveOptions{})
	if len(ec.Errors) != 1 || !errors.Is(ec.Errors[0], context.Canceled) {
		t.Fatalf("errors = %v, want one context.Canceled", ec.Errors)
	}
	if ec.Merged != nil {
		t.Fatalf("merged = %#v, want nil", ec.Merged)
	}
	for _, lr := range ec.LayerResults() {
		if lr.Config != nil || lr.Local != nil || len(lr.Errors) > 0 || len(lr.Reads) > 0 {
			t.Fatalf("layer result = %#v, want zero", lr)
		}
	}
}

func TestLayerIntentViews(t *testing.T) {
	dir := t.TempDir()
	v4, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
harnesses: [claude, devin]
allowed_registries: []
allowed_marketplaces: [https://mp-allowed.example]
sources:
  zdir: {directory: ./regs}
  agit: {git: https://example.com/repo.git, ref: main}
  mmp: {marketplace: https://market.example}
resources:
  agit: {all: true}
  mmp: {include: [main/collection/onboarding]}
  zdir: {all: true}
`), dir+"/config.yaml", true)
	if err != nil {
		t.Fatal(err)
	}
	li := LayerIntent{Source: UserPersonal, v4: v4}

	wantSources := []SourceDecl{
		{Kind: V4SourceGit, CanonicalURL: "https://example.com/repo"},
		{Kind: V4SourceMarketplace, CanonicalURL: "https://market.example"},
		{Kind: V4SourceDirectory},
	}
	if got := li.Sources(); !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("sources = %#v, want %#v", got, wantSources)
	}
	if !li.NeedsLock() {
		t.Fatal("v4 layer with git and acquiring marketplace sources must need a lock")
	}
	if !li.NeedsMarketplaceLock() {
		t.Fatal("v4 layer with an acquiring marketplace source must need a marketplace lock")
	}

	settings := li.Settings()
	if settings.AllowedRegistries == nil || len(*settings.AllowedRegistries) != 0 {
		t.Fatalf("allowed registries = %#v, want explicit empty", settings.AllowedRegistries)
	}
	if settings.AllowedMarketplaces == nil || !reflect.DeepEqual(*settings.AllowedMarketplaces, []string{"https://mp-allowed.example"}) {
		t.Fatalf("allowed marketplaces = %#v", settings.AllowedMarketplaces)
	}
	(*settings.AllowedMarketplaces)[0] = "mutated"
	if (*v4.AllowedMarketplaces)[0] != "https://mp-allowed.example" {
		t.Fatal("Settings must clone allowlists")
	}
	settings.Harnesses[0] = "mutated"
	if v4.Harnesses[0] != "claude" {
		t.Fatal("Settings must clone harnesses")
	}

	legacy := &Config{
		Registries: []RegistryEntry{
			{URL: "https://example.com/legacy.git"},
			{URL: dir + "/local-regs"},
		},
		Marketplaces: []MarketplaceEntry{{URL: "https://mp.example"}},
	}
	ll := LayerIntent{Source: UserPersonal, legacy: legacy}
	wantLegacySources := []SourceDecl{
		{Kind: V4SourceGit, CanonicalURL: "https://example.com/legacy"},
		{Kind: V4SourceDirectory},
		{Kind: V4SourceMarketplace, CanonicalURL: "https://mp.example"},
	}
	if got := ll.Sources(); !reflect.DeepEqual(got, wantLegacySources) {
		t.Fatalf("legacy sources = %#v, want %#v", got, wantLegacySources)
	}
	if !ll.NeedsLock() {
		t.Fatal("legacy layer with a remote git registry must need a lock")
	}
	if ll.NeedsMarketplaceLock() {
		t.Fatal("legacy layer must not need a marketplace lock")
	}
}

func TestBindPreservesLayerErrorOrder(t *testing.T) {
	orgEvidenceErr := errors.New("organization evidence error")
	userReadErr := errors.New("user config read error")

	intent := Intent{
		Layers: [5]LayerIntent{
			Organization:  {Source: Organization, legacy: &Config{}},
			UserPersonal:  {Source: UserPersonal, errors: []error{userReadErr}},
			RepoCommitted: {Source: RepoCommitted},
			RepoPersonal:  {Source: RepoPersonal},
		},
	}
	for _, source := range intentLayerOrder {
		intent.Errors = append(intent.Errors, intent.Layers[source].errors...)
	}

	ev := Evidence{}
	ev.errors[Organization] = []error{orgEvidenceErr}

	ec := Bind(intent, ev)

	want := append([]error{}, ec.Organization.Errors...)
	want = append(want, ec.User.Errors...)
	want = append(want, ec.Repo.Errors...)
	want = append(want, ec.LocalLayer.Errors...)
	if len(ec.Errors) != len(want) {
		t.Fatalf("errors = %v, want %v", ec.Errors, want)
	}
	for i := range want {
		if ec.Errors[i] != want[i] {
			t.Fatalf("errors[%d] = %v, want %v (full errors %v)", i, ec.Errors[i], want[i], ec.Errors)
		}
	}
	if len(ec.Errors) != 2 || ec.Errors[0] != orgEvidenceErr || ec.Errors[1] != userReadErr {
		t.Fatalf("errors = %v, want [organization evidence error, user read error]", ec.Errors)
	}
	counts := map[error]int{}
	for _, err := range ec.Errors {
		counts[err]++
	}
	if counts[orgEvidenceErr] != 1 || counts[userReadErr] != 1 {
		t.Fatalf("errors = %v, want each error exactly once", ec.Errors)
	}
}

func TestIntentViews(t *testing.T) {
	org := LayerIntent{Source: Organization, legacy: &Config{Harnesses: []string{"cursor"}}}
	user := LayerIntent{Source: UserPersonal, legacy: &Config{Harnesses: []string{"devin"}}}
	repo := LayerIntent{Source: RepoCommitted, legacy: &Config{Harnesses: []string{"claude"}}}

	intent := Intent{Layers: [5]LayerIntent{Organization: org, UserPersonal: user, RepoCommitted: repo}}
	harnesses, source := intent.TargetHarnesses()
	if source != RepoCommitted || !reflect.DeepEqual(harnesses, []string{"claude"}) {
		t.Fatalf("target harnesses = %v from %v, want [claude] from repo", harnesses, source)
	}
	harnesses[0] = "mutated"
	if repo.legacy.Harnesses[0] != "claude" {
		t.Fatal("TargetHarnesses must return a clone")
	}

	intent.Layers[RepoCommitted] = LayerIntent{Source: RepoCommitted}
	harnesses, source = intent.TargetHarnesses()
	if source != UserPersonal || !reflect.DeepEqual(harnesses, []string{"devin"}) {
		t.Fatalf("target harnesses = %v from %v, want [devin] from user", harnesses, source)
	}

	intent.Layers[UserPersonal] = LayerIntent{Source: UserPersonal}
	harnesses, source = intent.TargetHarnesses()
	if source != Organization || !reflect.DeepEqual(harnesses, []string{"cursor"}) {
		t.Fatalf("target harnesses = %v from %v, want [cursor] from organization", harnesses, source)
	}

	intent.Layers[Organization] = LayerIntent{Source: Organization}
	if harnesses, source := intent.TargetHarnesses(); harnesses != nil || source != SourceUnset {
		t.Fatalf("target harnesses = %v from %v, want unset", harnesses, source)
	}
}

func writeIntentV4Files(t *testing.T, dir string) (rootPath, includePath string, rootBytes, includeBytes []byte) {
	t.Helper()
	includePath = filepath.Join(dir, "fragment.yaml")
	rootPath = filepath.Join(dir, "config.yaml")
	includeBytes = []byte(`apiVersion: agent-marketplace/v4
sources:
  frag: {directory: ./frag-regs}
resources:
  frag: {all: true}
`)
	rootBytes = []byte(`apiVersion: agent-marketplace/v4
include: [fragment.yaml]
sources:
  main: {directory: ./regs}
resources:
  main: {all: true}
`)
	if err := os.WriteFile(includePath, includeBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPath, rootBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return rootPath, includePath, rootBytes, includeBytes
}

func TestReadLayerIntentV4CapturesReadsAtParse(t *testing.T) {
	dir := t.TempDir()
	rootPath, includePath, rootBytes, includeBytes := writeIntentV4Files(t, dir)
	rootCanonical, err := canonicalConfigPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	includeCanonical, err := canonicalConfigPath(includePath)
	if err != nil {
		t.Fatal(err)
	}

	includeReads := 0
	original := v4ReadConfigFile
	v4ReadConfigFile = func(path string) ([]byte, error) {
		if path == includeCanonical {
			includeReads++
			if includeReads > 1 {
				return nil, errors.New("v4 include reread after parse")
			}
		}
		return original(path)
	}
	t.Cleanup(func() { v4ReadConfigFile = original })

	li, err := readLayerIntent(rootPath, rootCanonical, rootBytes, repoLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if includeReads != 1 {
		t.Fatalf("include read count = %d, want exactly 1", includeReads)
	}
	if len(li.Reads) != len(li.v4.Files) {
		t.Fatalf("reads = %#v not parallel to files = %#v", li.Reads, li.v4.Files)
	}
	for i := range li.v4.Files {
		if li.Reads[i].Path != li.v4.Files[i] {
			t.Fatalf("read %d path %q does not match file %q", i, li.Reads[i].Path, li.v4.Files[i])
		}
	}
	byPath := make(map[string]ConfigRead, len(li.Reads))
	for _, read := range li.Reads {
		byPath[read.Path] = read
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	// Includes are requested relative to the canonical root directory.
	includeAbs := filepath.Join(filepath.Dir(rootCanonical), "fragment.yaml")
	rootRead, ok := byPath[rootCanonical]
	if !ok {
		t.Fatalf("missing root read in %#v", li.Reads)
	}
	if rootRead.RequestedPath != rootAbs || rootRead.Digest != fmt.Sprintf("%x", sha256.Sum256(rootBytes)) || rootRead.Overlay {
		t.Fatalf("root read = %#v, want requested %q digest of parsed bytes without overlay", rootRead, rootAbs)
	}
	includeRead, ok := byPath[includeCanonical]
	if !ok {
		t.Fatalf("missing include read in %#v", li.Reads)
	}
	if includeRead.RequestedPath != includeAbs || includeRead.Digest != fmt.Sprintf("%x", sha256.Sum256(includeBytes)) || includeRead.Overlay {
		t.Fatalf("include read = %#v, want requested %q digest of parsed bytes without overlay", includeRead, includeAbs)
	}
}

func TestReadLayerIntentV4OverlayIncludeRead(t *testing.T) {
	dir := t.TempDir()
	rootPath, includePath, rootBytes, includeBytes := writeIntentV4Files(t, dir)
	rootCanonical, err := canonicalConfigPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	includeCanonical, err := canonicalConfigPath(includePath)
	if err != nil {
		t.Fatal(err)
	}

	diskReads := 0
	original := v4ReadConfigFile
	v4ReadConfigFile = func(path string) ([]byte, error) {
		diskReads++
		return original(path)
	}
	t.Cleanup(func() { v4ReadConfigFile = original })

	li, err := readLayerIntent(rootPath, rootCanonical, rootBytes, repoLayer, map[string][]byte{includeCanonical: includeBytes})
	if err != nil {
		t.Fatal(err)
	}
	if diskReads != 0 {
		t.Fatalf("overlaid include hit disk %d times", diskReads)
	}
	var includeRead *ConfigRead
	for i := range li.Reads {
		if li.Reads[i].Path == includeCanonical {
			includeRead = &li.Reads[i]
		}
	}
	if includeRead == nil || !includeRead.Overlay || includeRead.Digest != fmt.Sprintf("%x", sha256.Sum256(includeBytes)) {
		t.Fatalf("overlaid include read = %#v in %#v", includeRead, li.Reads)
	}
}

func TestReadLayerIntentV4KeepsCapturedCanonicalAfterRemove(t *testing.T) {
	dir := t.TempDir()
	rootPath, _, rootBytes, _ := writeIntentV4Files(t, dir)
	canonical, data, err := readEffectiveLayer(rootPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rootPath); err != nil {
		t.Fatal(err)
	}
	li, err := readLayerIntent(rootPath, canonical, data, repoLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(li.Reads) == 0 || li.Reads[len(li.Reads)-1].Path != canonical {
		t.Fatalf("root read path = %#v, want captured canonical %q", li.Reads, canonical)
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	rootRead := li.Reads[len(li.Reads)-1]
	if rootRead.RequestedPath != rootAbs || rootRead.Digest != fmt.Sprintf("%x", sha256.Sum256(rootBytes)) {
		t.Fatalf("root read = %#v, want requested %q digest of captured bytes", rootRead, rootAbs)
	}
}

// spec: configuration-intent/captured-configuration-provenance
func TestReadLayerIntentV4SymlinkRetargetKeepsCapturedProvenance(t *testing.T) {
	dir := t.TempDir()
	targetA := filepath.Join(dir, "a")
	targetB := filepath.Join(dir, "b")
	if err := os.MkdirAll(targetA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetB, 0o700); err != nil {
		t.Fatal(err)
	}
	aBytes := []byte(`apiVersion: agent-marketplace/v4
sources:
  main: {directory: ./regs-a}
resources:
  main: {all: true}
`)
	bBytes := []byte(`apiVersion: agent-marketplace/v4
sources:
  main: {directory: ./regs-b}
resources:
  main: {all: true}
`)
	aPath := filepath.Join(targetA, "config.yaml")
	bPath := filepath.Join(targetB, "config.yaml")
	if err := os.WriteFile(aPath, aBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, bBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(aPath, link); err != nil {
		t.Fatal(err)
	}
	canonical, data, err := readEffectiveLayer(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	aCanonical, err := canonicalConfigPath(aPath)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != aCanonical {
		t.Fatalf("captured canonical = %q, want %q", canonical, aCanonical)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bPath, link); err != nil {
		t.Fatal(err)
	}
	li, err := readLayerIntent(link, canonical, data, repoLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	linkAbs, err := filepath.Abs(link)
	if err != nil {
		t.Fatal(err)
	}
	if len(li.Reads) != 1 {
		t.Fatalf("reads = %#v, want exactly the root read", li.Reads)
	}
	read := li.Reads[0]
	if read.Path != aCanonical || read.RequestedPath != linkAbs || read.Digest != fmt.Sprintf("%x", sha256.Sum256(aBytes)) {
		t.Fatalf("read = %#v, want canonical %q requested %q digest of A bytes", read, aCanonical, linkAbs)
	}
	if len(li.v4.Sources) != 1 {
		t.Fatalf("sources = %#v, want A document sources", li.v4.Sources)
	}
	if src, ok := li.v4.Sources["main"]; !ok || src.File != aCanonical {
		t.Fatalf("parsed sources = %#v, want A bytes", li.v4.Sources)
	}
}
