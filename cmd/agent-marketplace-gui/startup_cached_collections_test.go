package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
	startupfixture "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/startup"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

type work49CacheSeed struct {
	name         string
	members      []string
	verified     bool
	metadataTime time.Time
}

type work49OrgConfigBuilder func(*testing.T, *marketplace.Cache, string) []byte

func seedWork49Collection(t *testing.T, cache *marketplace.Cache, marketURL string, seed work49CacheSeed) (string, string) {
	t.Helper()
	members := make([]hccartifact.CollectionMemberEntry, len(seed.members))
	for i, name := range seed.members {
		members[i] = hccartifact.CollectionMemberEntry{Kind: "skill", Name: name, RegistryID: "tools", Revision: "sha256:" + strings.Repeat("a", 64)}
	}
	digest, _, _, metadataPath, manifestPath := startupfixture.SeedCollection(t, cache, marketURL, seed.name, members, seed.verified)
	if err := os.Chtimes(metadataPath, seed.metadataTime, seed.metadataTime); err != nil {
		t.Fatal("set collection metadata fixture time")
	}
	return digest, manifestPath
}

func work49LegacyOrganizationConfig(marketURL, collectionName, revision string) []byte {
	return []byte(fmt.Sprintf("apiVersion: agent-marketplace/v3\nharnesses: [devin]\ntelemetry:\n  level: off\nmarketplaces:\n  - url: %q\n    instance_id: mp\n    resources:\n      - registry_id: tools\n        registry_slug: tools\n        kind: collection\n        name: %q\n        revision: %q\n        digest: %q\n        acquisition: mirror\n        direct: true\n", marketURL, collectionName, revision, revision))
}

func work49ProvisionalRows(t *testing.T, userConfig func(source, marketURL string) string, buildOrg work49OrgConfigBuilder) []acquisitionHostRow {
	t.Helper()
	marketURL, _, release, closeMarket := blockedWork47Marketplace(t)
	paths := startupHostFixture(t, func(source string) string {
		if userConfig != nil {
			return userConfig(source, marketURL)
		}
		return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true}\n", source)
	})
	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "config.yaml")
	orgConfig := buildOrg(t, marketplace.DefaultCache(), marketURL)
	if err := os.WriteFile(orgPath, orgConfig, 0o600); err != nil {
		t.Fatal("write private organization collection fixture")
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	paths = append(paths, orgDir, orgPath, config.Organization.LockPath(orgPath))
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create host GUI for cached-collection fixture")
	}
	server := httptest.NewServer(app.handler())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var response *http.Response
	var readerDone chan struct{}
	cleanup := func() {
		release()
		cancel()
		if response != nil {
			_ = response.Body.Close()
		}
		app.close()
		server.Close()
		closeMarket()
		if readerDone != nil {
			select {
			case <-readerDone:
			case <-time.After(5 * time.Second):
				t.Error("host event reader did not stop after release")
			}
		}
	}
	t.Cleanup(cleanup)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal("create authenticated host event request")
	}
	request.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal("open authenticated host event stream")
	}
	if response.StatusCode != http.StatusOK {
		t.Fatal("host event stream returned unexpected status")
	}
	snapshots, failures, readerDone := make(chan acquisitionHostEvent, 5), make(chan struct{}, 1), make(chan struct{})
	go streamAcquisitionHostSnapshots(ctx, response.Body, snapshots, failures, readerDone)
	first := waitAcquisitionHostSnapshot(t, ctx, snapshots, failures, readerDone)
	if first.Type != "snapshot" || first.Generation == 0 || first.Payload.Stage != "provisional" || first.Payload.Drift != "pending" {
		t.Fatal("host did not publish a pending provisional snapshot")
	}
	assertProtectedHostPathsUnchanged(t, paths, before)
	cleanup()
	assertProtectedHostPathsUnchanged(t, paths, before)
	return first.Payload.Rows
}

func assertWork49Rows(t *testing.T, rows []acquisitionHostRow, wantOrgSkills []string) {
	t.Helper()
	local, organization := make([]string, 0, 2), make([]string, 0)
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.ID == "" || ids[row.ID] {
			t.Fatal("host snapshot rows did not have unique opaque identities")
		}
		ids[row.ID] = true
		if row.Scope == "user" && row.Kind == "skill" && row.Registry == "local" {
			if !row.Checked || !row.PendingVerification || row.ResourceAction != "none" {
				t.Fatal("local Directory row did not remain selected and pending")
			}
			local = append(local, row.Name)
		}
		if row.Scope == "organization" && row.Kind == "skill" {
			if !row.Checked || !row.PendingVerification || row.ResourceAction != "none" {
				t.Fatal("organization collection leaf did not remain selected and pending")
			}
			organization = append(organization, row.Name)
		}
	}
	slices.Sort(local)
	slices.Sort(organization)
	if !slices.Equal(local, []string{"alpha", "beta"}) {
		t.Fatal("provisional host snapshot lost the Directory control rows")
	}
	if !slices.Equal(organization, wantOrgSkills) {
		t.Fatal("organization skill leaves did not match the configured cached collection choice")
	}
}

// spec: session-startup/cached-collection-leaves-prefer-the-exact-then-newest-verified-revision
func TestGuiHostCachedCollectionRevisionChoice(t *testing.T) {
	zero := "sha256:" + strings.Repeat("0", 64)
	cases := []struct {
		name  string
		build work49OrgConfigBuilder
		want  []string
	}{
		{name: "exact-old", build: func(t *testing.T, cache *marketplace.Cache, url string) []byte {
			old, _ := seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"old-only"}, verified: true, metadataTime: time.Unix(10, 0)})
			seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"new-only"}, verified: true, metadataTime: time.Unix(20, 0)})
			return work49LegacyOrganizationConfig(url, "bundle", old)
		}, want: []string{"old-only"}},
		{name: "newest-skips-unverified", build: func(t *testing.T, cache *marketplace.Cache, url string) []byte {
			seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"old-only"}, verified: true, metadataTime: time.Unix(10, 0)})
			seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"new-only"}, verified: true, metadataTime: time.Unix(20, 0)})
			seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"unverified-only"}, metadataTime: time.Unix(30, 0)})
			return work49LegacyOrganizationConfig(url, "bundle", zero)
		}, want: []string{"new-only"}},
		{name: "missing", build: func(t *testing.T, cache *marketplace.Cache, url string) []byte {
			seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"old-only"}, verified: true, metadataTime: time.Unix(10, 0)})
			seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"new-only"}, verified: true, metadataTime: time.Unix(20, 0)})
			return work49LegacyOrganizationConfig(url, "missing", zero)
		}, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertWork49Rows(t, work49ProvisionalRows(t, nil, tc.build), tc.want)
		})
	}
}

// spec: session-startup/tampered-cached-manifests-are-rejected
func TestGuiHostRejectsTamperedCachedCollection(t *testing.T) {
	rows := work49ProvisionalRows(t, nil, func(t *testing.T, cache *marketplace.Cache, url string) []byte {
		digest, manifestPath := seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"lone"}, verified: true, metadataTime: time.Unix(10, 0)})
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal("read test-owned collection manifest")
		}
		tampered := []byte(strings.Replace(string(raw), "lone", "tampered", 1))
		if bytes.Equal(raw, tampered) {
			t.Fatal("tamper fixture did not alter the manifest")
		}
		if err := os.WriteFile(manifestPath, tampered, 0o644); err != nil {
			t.Fatal("tamper test-owned manifest")
		}
		return work49LegacyOrganizationConfig(url, "bundle", digest)
	})
	assertWork49Rows(t, rows, nil)
}

// spec: session-startup/organization-marketplace-collection-expands-from-the-verified-cached-manifest
func TestGuiHostExpandsOrganizationCollectionFromVerifiedCache(t *testing.T) {
	zero := "sha256:" + strings.Repeat("0", 64)
	rows := work49ProvisionalRows(t, func(source, marketURL string) string {
		return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\n  mp: {marketplace: %q}\nresources:\n  local: {all: true}\n  mp:\n    exclude: [tools/skill/review]\n", source, marketURL)
	}, func(t *testing.T, cache *marketplace.Cache, url string) []byte {
		seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"audit", "review"}, verified: true, metadataTime: time.Unix(10, 0)})
		return work49LegacyOrganizationConfig(url, "bundle", zero)
	})
	local, organization := make([]string, 0, 2), make([]string, 0, 2)
	var audit, review acquisitionHostRow
	for _, row := range rows {
		if row.Scope == "user" && row.Kind == "skill" && row.Registry == "local" {
			if !row.Checked || !row.PendingVerification || row.ResourceAction != "none" {
				t.Fatal("local Directory row did not remain selected and pending")
			}
			local = append(local, row.Name)
		}
		if row.Scope != "organization" || row.Kind != "skill" {
			continue
		}
		organization = append(organization, row.Name)
		switch row.Name {
		case "audit":
			audit = row
		case "review":
			review = row
		}
	}
	slices.Sort(local)
	if !slices.Equal(local, []string{"alpha", "beta"}) {
		t.Fatal("provisional host snapshot lost the Directory control rows")
	}
	if !slices.Equal(organization, []string{"audit", "review"}) {
		t.Fatal("organization collection did not expand to its cached members in verified order")
	}
	if !audit.Checked || !audit.PendingVerification || audit.ResourceAction != "none" {
		t.Fatal("cached organization member did not render as selected pending work")
	}
	if review.Checked || !review.PendingVerification || review.ResourceAction != "none" || !strings.Contains(review.Status, "excluded") {
		t.Fatal("user-excluded organization member did not render as excluded pending work")
	}
}

// spec: session-startup/optional-collection-member-yields-to-a-configured-selection-of-the-same-resource
func TestGuiHostCollectionMemberYieldsToConfiguredSelection(t *testing.T) {
	zero := "sha256:" + strings.Repeat("0", 64)
	rows := work49ProvisionalRows(t, nil, func(t *testing.T, cache *marketplace.Cache, url string) []byte {
		seedWork49Collection(t, cache, url, work49CacheSeed{name: "bundle", members: []string{"alpha", "gamma"}, verified: true, metadataTime: time.Unix(10, 0)})
		return work49LegacyOrganizationConfig(url, "bundle", zero)
	})
	assertWork49Rows(t, rows, []string{"gamma"})
	alphas := 0
	for _, row := range rows {
		if row.Name == "alpha" {
			alphas++
		}
	}
	if alphas != 1 {
		t.Fatal("colliding collection member did not yield to the configured selection")
	}
}
