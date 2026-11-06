package localapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/guiupdate"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func requestUpdateEndpoint(t *testing.T, api *API, requestBody string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(requestBody))
	req.Header.Set(tokenHeader, api.Credential())
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("response is not JSON: %q", recorder.Body.String())
	}
	return recorder.Code, response
}

func TestUpdateStartRejectsClientProvidedURLs(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	req := httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(
		`{"action":"start","assetURL":"https://attacker.example/payload","checksumsURL":"https://attacker.example/SHA256SUMS"}`,
	))
	req.Header.Set(tokenHeader, api.Credential())
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

type updateDownloadTransport struct {
	archive   []byte
	checksums []byte
	requests  []string
}

func (d *updateDownloadTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	d.requests = append(d.requests, r.URL.String())
	body := d.archive
	if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
		body = d.checksums
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    r,
	}, nil
}

type updateTestLinuxPlatform struct {
	downloadDir string
	opened      []string
}

func (p *updateTestLinuxPlatform) DownloadDir() (string, error) { return p.downloadDir, nil }
func (p *updateTestLinuxPlatform) OpenPackage(_ context.Context, path string) error {
	p.opened = append(p.opened, path)
	return nil
}

// spec: gui-shell/user-hands-the-verified-package-to-the-software-installer
func TestUpdateStartUsesServerResolvedReleaseAndStatusEndpoints(t *testing.T) {
	api, stub, _, _ := newReleaseAPIWithPackageFormat(t, "1.2.3", "deb")
	assetName := fmt.Sprintf("agent-marketplace-gui-v1.3.0-linux-%s.deb", runtime.GOARCH)
	archive := []byte("verified deb")
	digest := sha256.Sum256(archive)
	pageURL := "https://github.com/Adyen/agent-marketplace/releases/tag/v1.3.0"
	stub.body = releaseBodyWithAssets("v1.3.0", pageURL, assetName, "SHA256SUMS")

	dir := t.TempDir()
	downloads := &updateDownloadTransport{
		archive:   archive,
		checksums: []byte(fmt.Sprintf("%x  %s\n", digest, assetName)),
	}
	platform := &updateTestLinuxPlatform{downloadDir: dir}
	updater := guiupdate.New(guiupdate.Config{
		Format:       "deb",
		Architecture: runtime.GOARCH,
		Client:       &http.Client{Transport: downloads},
		Linux:        platform,
	})
	api.updater = updater

	status, started := requestUpdateEndpoint(t, api, `{"action":"start"}`)
	if status != http.StatusOK || started["phase"] != string(guiupdate.PhaseDownloading) {
		t.Fatalf("start = %d, %v", status, started)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := updater.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	status, result := requestUpdateEndpoint(t, api, `{"action":"status"}`)
	if status != http.StatusOK || result["phase"] != string(guiupdate.PhaseComplete) {
		t.Fatalf("status = %d, %v", status, result)
	}
	wantRequests := []string{
		"https://github.com/Adyen/agent-marketplace/releases/download/v1.3.0/" + assetName,
		"https://github.com/Adyen/agent-marketplace/releases/download/v1.3.0/SHA256SUMS",
	}
	if strings.Join(downloads.requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("download requests = %v, want %v", downloads.requests, wantRequests)
	}
	if len(platform.opened) != 1 {
		t.Fatalf("opened packages = %v, want one", platform.opened)
	}
	saved, err := os.ReadFile(filepath.Join(dir, assetName))
	if err != nil || string(saved) != string(archive) {
		t.Fatalf("saved package = %q, %v", saved, err)
	}
}

func TestReleaseNoticeIncludesResolvedMacOSDMGURL(t *testing.T) {
	api, stub, _, _ := newReleaseAPI(t, "1.2.3")
	stub.body = releaseBodyWithAssets(
		"v1.3.0",
		"https://github.com/Adyen/agent-marketplace/releases/tag/v1.3.0",
		"agent-marketplace-gui-v1.3.0-macos-universal.zip",
		"agent-marketplace-gui-v1.3.0-macos-universal.dmg",
		"SHA256SUMS",
	)

	status, body := requestRelease(t, api, nil)
	if status != http.StatusOK {
		t.Fatalf("release status = %d, want 200", status)
	}
	want := "https://github.com/Adyen/agent-marketplace/releases/download/v1.3.0/agent-marketplace-gui-v1.3.0-macos-universal.dmg"
	if got, _ := body["dmgURL"].(string); got != want {
		t.Fatalf("dmgURL = %q, want %q", got, want)
	}
}
