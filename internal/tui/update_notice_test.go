package tui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ansi "github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/updatecheck"
)

const upgradeNoticeTestRelease = `{"tag_name":"v4.1.4","published_at":"2026-09-28T10:00:00Z","html_url":"https://github.com/Adyen/agent-marketplace/releases/tag/v4.1.4"}`

// upgradeNoticeChecker returns a checker running v4.1.3 against endpoint whose
// cache holds checkedAt and, when withLatest is set, a v4.1.4 release.
func upgradeNoticeChecker(t *testing.T, endpoint string, checkedAt time.Time, withLatest bool) *updatecheck.Checker {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), updatecheck.StateFileName)
	latest := `null`
	if withLatest {
		latest = `{"tag":"v4.1.4","published_at":"2026-09-28T10:00:00Z","url":"https://github.com/Adyen/agent-marketplace/releases/tag/v4.1.4"}`
	}
	body := fmt.Sprintf(`{"checked_at":%q,"latest":%s}`, checkedAt.Format(time.RFC3339), latest)
	if err := os.WriteFile(statePath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	checker := updatecheck.New("v4.1.3", statePath)
	checker.Endpoint = endpoint
	return checker
}

// spec: upgrade-notice/tui-shows-an-available-release-once
func TestUpgradeNoticeShowsOnceAsBoundedNotification(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, upgradeNoticeTestRelease)
	}))
	t.Cleanup(srv.Close)
	checker := upgradeNoticeChecker(t, srv.URL, time.Now(), true)

	m := NewModel("/repo", "/home", WithUpgradeCheck(checker))
	updated, _ := m.Update(upgradeNoticeCmd(checker)())
	toasts := updated.(Model).visibleToasts()
	if len(toasts) != 1 || !strings.Contains(toasts[0].text, "v4.1.4 is available") {
		t.Fatalf("toasts = %+v, want one notice naming v4.1.4", toasts)
	}
	if msg := upgradeNoticeCmd(checker)(); msg != nil {
		t.Fatalf("second notice within 24h = %#v, want none", msg)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("fresh cache made %d requests, want 0", n)
	}
}

func TestUpgradeNoticeDoesNotBlockFirstFrame(t *testing.T) {
	release := make(chan struct{})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = io.WriteString(w, upgradeNoticeTestRelease)
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	checker := upgradeNoticeChecker(t, srv.URL, time.Now().Add(-48*time.Hour), false)

	m := NewModel("/repo", "/home", WithUpgradeCheck(checker))
	if m.Init() == nil {
		t.Fatal("Init returned no commands, want the notice command among them")
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("Init made %d requests before any command ran, want 0", n)
	}
	if out := ansi.Strip(m.View().Content); out == "" {
		t.Fatal("first frame is empty while the lookup is pending")
	}
}
