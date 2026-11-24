package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestLockedGitPlaceProfileReaderUsesPinnedCommit(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "--object-format=sha1", "--initial-branch=main")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	profilePath := filepath.Join(repo, "profiles", "invented-app.yaml")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	first := []byte(placeAdmissionProfileYAML)
	if err := os.WriteFile(profilePath, first, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "profiles/invented-app.yaml")
	runGit(t, repo, "commit", "-m", "add profile")
	firstCommit := gitCommitForTest(t, repo)

	second := bytes.Replace(first, []byte("label: Invented App"), []byte("label: Changed App"), 1)
	if err := os.WriteFile(profilePath, second, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "profiles/invented-app.yaml")
	runGit(t, repo, "commit", "-m", "change profile")

	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	rawURL := (&url.URL{Scheme: "file", Path: canonicalRepo}).String()
	origin, err := gitidentity.CanonicalV1(rawURL)
	if err != nil {
		t.Fatalf("canonicalize local repo URL: %v", err)
	}
	ref := "refs/heads/main"
	root := "profiles/invented-app.yaml"
	commit, err := lockfile.ParseGitOID("sha1:" + firstCommit)
	if err != nil {
		t.Fatal(err)
	}
	locked := lockfile.GitRegistry{URL: origin, Ref: ref, Commit: commit}
	identity := targetprofile.Identity{
		TargetID: "invented-app", SourceID: gitPlaceProfileSourceID(origin, ref, root),
		RevisionDigest: profileTestDigest(first),
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	exactCacheRoot := filepath.Join(cache.CacheDir(), "exact-git")

	for _, bad := range []struct {
		name     string
		identity targetprofile.Identity
	}{
		{
			name: "different origin",
			identity: targetprofile.Identity{
				TargetID:       identity.TargetID,
				SourceID:       gitPlaceProfileSourceID(mustCanonicalGitURL(t, filepath.Join(home, "other-repo")), ref, root),
				RevisionDigest: identity.RevisionDigest,
			},
		},
		{
			name: "different ref",
			identity: targetprofile.Identity{
				TargetID:       identity.TargetID,
				SourceID:       gitPlaceProfileSourceID(origin, "refs/heads/other", root),
				RevisionDigest: identity.RevisionDigest,
			},
		},
	} {
		t.Run(bad.name, func(t *testing.T) {
			reader := LockedGitPlaceProfileReader(locked, rawURL)
			if _, err := reader(context.Background(), bad.identity, resolver.MarketplaceResolveModeRefresh); err == nil ||
				!strings.Contains(err.Error(), "marketplace.profile.identity-mismatch") {
				t.Fatalf("reader error = %v, want source identity refusal", err)
			}
			if _, err := os.Stat(exactCacheRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("source mismatch touched exact Git cache: %v", err)
			}
		})
	}
	otherFetchURL := mustCanonicalGitURL(t, filepath.Join(home, "other-fetch"))
	if _, err := LockedGitPlaceProfileReader(locked, otherFetchURL)(
		context.Background(), identity, resolver.MarketplaceResolveModeRefresh,
	); err == nil || !strings.Contains(err.Error(), "marketplace.profile.identity-mismatch") {
		t.Fatalf("mismatched fetch URL error = %v", err)
	}
	if _, err := os.Stat(exactCacheRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched fetch URL touched exact Git cache: %v", err)
	}

	reader := LockedGitPlaceProfileReader(locked, rawURL)
	got, err := reader(context.Background(), identity, resolver.MarketplaceResolveModeRefresh)
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("online pinned profile = %q, %v; want original commit bytes", got, err)
	}
	offline := LockedGitPlaceProfileReader(locked, "")
	got, err = offline(context.Background(), identity, resolver.MarketplaceResolveModeCachedNoFetch)
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("offline pinned profile = %q, %v; want original commit bytes", got, err)
	}
	admissionFixture := newPlaceAdmissionFixture(t)
	entry := admissionFixture.config.Marketplaces[0]
	entry.Resources = append([]config.MarketplaceResourcePin(nil), entry.Resources...)
	for i := range entry.Resources {
		switch entry.Resources[i].Kind {
		case "target-profile":
			entry.Resources[i].LockProfileIdentity = identity
		case "invented-app/mod":
			binding := entry.Resources[i].LockHostPackageIdentity
			binding.ProfileIdentity = identity
			entry.Resources[i].LockHostPackageIdentity = binding
		}
	}
	admissionFixture.config.Marketplaces = []config.MarketplaceEntry{entry}
	admission, err := PreparePlaceAdmission(
		context.Background(), admissionFixture.config, []string{identity.TargetID},
		admissionFixture.env, offline, admissionFixture.resolver,
		resolver.MarketplaceResolveModeCachedNoFetch,
	)
	if err != nil || len(admission.Pending) != 1 || admission.Pending[0] != identity ||
		len(admission.Approvals) != 0 || len(admission.Packages) != 0 {
		t.Fatalf("offline admission with Git profile = %#v, %v", admission, err)
	}
	if admissionFixture.requests.requests.Load() != 0 {
		t.Fatalf("offline admission issued %d HTTP requests", admissionFixture.requests.requests.Load())
	}
	if _, err := offline(context.Background(), identity, resolver.MarketplaceResolveModeCached); err == nil {
		t.Fatal("unsupported mode succeeded")
	}

	wrongDigest := identity
	wrongDigest.RevisionDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := offline(context.Background(), wrongDigest, resolver.MarketplaceResolveModeCachedNoFetch); err == nil ||
		!strings.Contains(err.Error(), "marketplace.profile.digest-mismatch") {
		t.Fatalf("wrong profile digest error = %v", err)
	}

	targetPath := filepath.Join(repo, "profiles", "target.yaml")
	if err := os.WriteFile(targetPath, first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(profilePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.yaml", profilePath); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "replace profile with symlink")
	symlinkCommit, err := lockfile.ParseGitOID("sha1:" + gitCommitForTest(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	symlinkLock := locked
	symlinkLock.Commit = symlinkCommit
	if _, err := LockedGitPlaceProfileReader(symlinkLock, rawURL)(context.Background(), identity, resolver.MarketplaceResolveModeRefresh); err == nil ||
		!strings.Contains(err.Error(), "marketplace.profile.unavailable") {
		t.Fatalf("symlinked profile error = %v", err)
	}
}

func gitPlaceProfileSourceID(origin, ref, root string) string {
	return "git:" + url.QueryEscape(origin) + "@" + url.QueryEscape(ref) + "#" + url.QueryEscape(root)
}

func mustCanonicalGitURL(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := (&url.URL{Scheme: "file", Path: canonical}).String()
	result, err := gitidentity.CanonicalV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func profileTestDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func gitCommitForTest(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(output))
}
