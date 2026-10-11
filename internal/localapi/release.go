package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Adyen/agent-marketplace/internal/config"
	"golang.org/x/mod/semver"
)

const releaseURL = "https://api.github.com/repos/Adyen/agent-marketplace/releases/latest"

type UserOption func(*API)

func WithVersion(version string) UserOption {
	return func(a *API) { a.version = version }
}

func normalizeReleaseVersion(version string) string {
	if version == "" {
		return ""
	}
	if version[0] != 'v' {
		version = "v" + version
	}
	if !semver.IsValid(version) {
		return ""
	}
	return semver.Canonical(version)
}

func (a *API) checkLatestRelease(r *http.Request) string {
	running := normalizeReleaseVersion(a.version)
	if running == "" {
		return ""
	}
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agent-marketplace-gui/"+a.version)
	resp, err := a.releaseClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var latest struct {
		TagName    string `json:"tag_name"`
		Draft      *bool  `json:"draft"`
		Prerelease *bool  `json:"prerelease"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20))
	if err := decoder.Decode(&latest); err != nil {
		return ""
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ""
	}
	if latest.Draft == nil || latest.Prerelease == nil || *latest.Draft || *latest.Prerelease {
		return ""
	}
	tag := normalizeReleaseVersion(latest.TagName)
	if tag == "" || semver.Prerelease(tag) != "" {
		return ""
	}
	if semver.Compare(tag, running) <= 0 {
		return ""
	}
	return tag
}

func (a *API) handleRelease(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.ContentLength != 0 {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	settings, err := config.LoadOrganizationSettings()
	if err != nil || !config.GUIReleaseCheckEnabled(settings) {
		writeReleaseNotice(w, "")
		return
	}
	a.releaseOnce.Do(func() {
		a.releaseNotice = a.checkLatestRelease(r)
	})
	writeReleaseNotice(w, a.releaseNotice)
}

func writeReleaseNotice(w http.ResponseWriter, latestVersion string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"latestVersion": latestVersion})
}
