package updatecheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	latestTag     = "v4.1.4"
	latestURL     = "https://github.com/Adyen/agent-marketplace/releases/tag/v4.1.4"
	pseudoVersion = "v0.0.0-20260927180313-baeda4e602ab"
)

var pseudoCommit = time.Date(2026, 9, 27, 18, 3, 13, 0, time.UTC)

func releaseBody(tag string, publishedAt time.Time) string {
	data, _ := json.Marshal(map[string]any{
		"tag_name":     tag,
		"published_at": publishedAt.Format(time.RFC3339),
		"html_url":     "https://github.com/Adyen/agent-marketplace/releases/tag/" + tag,
		"name":         "ignored",
	})
	return string(data)
}

// countingServer answers every request with body and counts the requests.
func countingServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// testChecker returns a Checker pointed at endpoint with a controllable clock and a private state file.
func testChecker(t *testing.T, version, endpoint string, clock *time.Time) (*Checker, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state", StateFileName)
	c := New(version, statePath)
	c.Endpoint = endpoint
	c.Now = func() time.Time { return *clock }
	return c, statePath
}

func seedState(t *testing.T, path string, st state) {
	t.Helper()
	if err := saveState(path, st); err != nil {
		t.Fatal(err)
	}
}

func runOnce(t *testing.T, c *Checker, maxWait time.Duration) string {
	t.Helper()
	return c.Start(context.Background()).Finish(maxWait)
}

// spec: upgrade-notice/release-build-behind-the-latest-release
func TestReleaseBuildBehindLatestReleaseNotices(t *testing.T) {
	published := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	got, ok := Notice("v4.1.3", Release{Tag: latestTag, PublishedAt: published, URL: latestURL})
	if !ok {
		t.Fatal("expected a notice for v4.1.3 behind v4.1.4")
	}
	want := "info: agent-marketplace v4.1.4 is available (you have v4.1.3): " + latestURL
	if got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

// spec: upgrade-notice/release-build-at-or-ahead-of-the-latest-release
func TestReleaseBuildAtOrAheadOfLatestReleaseIsSilent(t *testing.T) {
	latest := Release{Tag: latestTag, PublishedAt: time.Now(), URL: latestURL}
	for _, current := range []string{"v4.1.4", "v4.2.0", "v5.0.0"} {
		if msg, ok := Notice(current, latest); ok {
			t.Errorf("Notice(%s) = %q, want none", current, msg)
		}
	}
}

// spec: upgrade-notice/pseudo-version-built-before-the-latest-release
func TestPseudoVersionBuiltBeforeLatestReleaseNotices(t *testing.T) {
	latest := Release{Tag: latestTag, PublishedAt: pseudoCommit.Add(time.Second), URL: latestURL}
	got, ok := Notice(pseudoVersion, latest)
	if !ok {
		t.Fatal("expected a notice for a pseudo-version built before the release")
	}
	want := "info: agent-marketplace v4.1.4 was released after this build (2026-09-27); run: go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest"
	if got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

// spec: upgrade-notice/pseudo-version-built-after-the-latest-release
func TestPseudoVersionBuiltAtOrAfterLatestReleaseIsSilent(t *testing.T) {
	for name, published := range map[string]time.Time{
		"at":    pseudoCommit,
		"after": pseudoCommit.Add(-time.Second),
	} {
		latest := Release{Tag: latestTag, PublishedAt: published, URL: latestURL}
		if msg, ok := Notice(pseudoVersion, latest); ok {
			t.Errorf("%s: Notice = %q, want none", name, msg)
		}
	}
}

// spec: upgrade-notice/development-builds-are-never-checked
func TestDevelopmentBuildsAreNeverChecked(t *testing.T) {
	srv, hits := countingServer(t, releaseBody(latestTag, time.Now().Add(-time.Hour)))
	clock := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	for _, version := range []string{"dev", "v4.0.9-3-gbaeda4e", "v4.1.3-dirty", "v4.1.3+dirty", "v4.1.3-rc1", "v4.1"} {
		c, _ := testChecker(t, version, srv.URL, &clock)
		if msg := runOnce(t, c, time.Second); msg != "" {
			t.Errorf("%s: notice = %q, want none", version, msg)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("development builds made %d requests, want 0", n)
	}
}

// spec: upgrade-notice/fresh-cache-avoids-network-access
func TestFreshCacheAvoidsNetworkAccess(t *testing.T) {
	srv, hits := countingServer(t, releaseBody(latestTag, time.Now()))
	clock := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	c, statePath := testChecker(t, "v4.1.3", srv.URL, &clock)
	seedState(t, statePath, state{CheckedAt: clock.Add(-time.Hour)})

	runOnce(t, c, time.Second)
	if n := hits.Load(); n != 0 {
		t.Fatalf("fresh cache made %d requests, want 0", n)
	}
}

// spec: upgrade-notice/failed-lookup-is-silent-and-backs-off
func TestFailedLookupIsSilentAndBacksOff(t *testing.T) {
	previous := &Release{Tag: "v4.1.3", PublishedAt: pseudoCommit, URL: latestURL}
	for name, handler := range map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"malformed":    func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{not json") },
		"rate limited": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		},
		"slow": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				handler(w, r)
			}))
			t.Cleanup(srv.Close)
			clock := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
			c, statePath := testChecker(t, "v4.1.3", srv.URL, &clock)
			c.Client = &http.Client{Timeout: 200 * time.Millisecond}
			seedState(t, statePath, state{Latest: previous})

			if msg := runOnce(t, c, 3*time.Second); msg != "" {
				t.Fatalf("notice = %q, want none", msg)
			}
			st := loadState(statePath)
			if !st.CheckedAt.Equal(clock) {
				t.Fatalf("checked_at = %v, want attempt time %v", st.CheckedAt, clock)
			}
			if st.Latest == nil || st.Latest.Tag != previous.Tag || st.Latest.URL != previous.URL || !st.Latest.PublishedAt.Equal(previous.PublishedAt) {
				t.Fatalf("latest = %+v, want previous %+v", st.Latest, previous)
			}

			clock = clock.Add(time.Hour)
			runOnce(t, c, 3*time.Second)
			if n := hits.Load(); n != 1 {
				t.Fatalf("requests = %d, want 1 (no retry within 24h)", n)
			}
		})
	}
}

// spec: upgrade-notice/slow-lookup-does-not-delay-exit-beyond-the-bound
func TestSlowLookupDoesNotDelayExitBeyondBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	clock := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	c, _ := testChecker(t, "v4.1.3", srv.URL, &clock)

	start := time.Now()
	runOnce(t, c, time.Second)
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("Finish returned after %v, want about 1s", elapsed)
	}
}

// spec: upgrade-notice/notice-is-written-once-per-day-on-a-terminal
func TestNoticeIsThrottledToOncePerDay(t *testing.T) {
	srv, hits := countingServer(t, releaseBody(latestTag, time.Now().Add(-time.Hour)))
	clock := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	c, statePath := testChecker(t, "v4.1.3", srv.URL, &clock)
	seedState(t, statePath, state{CheckedAt: clock, Latest: &Release{Tag: latestTag, PublishedAt: clock.Add(-time.Hour), URL: latestURL}})

	first := runOnce(t, c, time.Second)
	if !strings.HasPrefix(first, "info: agent-marketplace v4.1.4 is available") {
		t.Fatalf("first run notice = %q", first)
	}
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state file mode = %o, want 600", perm)
	}

	clock = clock.Add(time.Hour)
	if msg := runOnce(t, c, time.Second); msg != "" {
		t.Fatalf("second run within 24h notice = %q, want none", msg)
	}

	clock = clock.Add(24 * time.Hour)
	if msg := runOnce(t, c, time.Second); msg == "" {
		t.Fatal("run after 24h notice = none, want notice again")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1 (only the lookup due after 24h)", n)
	}
}

func TestCorruptStateIsTreatedAsEmpty(t *testing.T) {
	srv, hits := countingServer(t, releaseBody(latestTag, time.Now()))
	clock := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	c, statePath := testChecker(t, "v4.1.3", srv.URL, &clock)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}

	runOnce(t, c, time.Second)
	if n := hits.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1 after corrupt state", n)
	}
}
