package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/updatecheck"
	"golang.org/x/mod/semver"
)

const releaseURL = "https://api.github.com/repos/Adyen/agent-marketplace/releases/latest"

type UserOption func(*API)

func WithVersion(version string) UserOption {
	return func(a *API) { a.version = version }
}

func WithPackageFormat(format string) UserOption {
	return func(a *API) { a.packageFormat = format }
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

type releaseNotice struct {
	LatestVersion  string `json:"latestVersion"`
	ReleasePageURL string `json:"releasePageURL"`
	AssetURL       string `json:"assetURL"`
	ChecksumsURL   string `json:"checksumsURL"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func guiReleaseAssetName(packageFormat, arch, tag string) string {
	switch packageFormat {
	case "macos-app":
		return "agent-marketplace-gui-" + tag + "-macos-universal.zip"
	case "rpm", "deb":
		if arch != "amd64" && arch != "arm64" {
			return ""
		}
		return "agent-marketplace-gui-" + tag + "-linux-" + arch + "." + packageFormat
	default:
		return ""
	}
}

func isGitHubReleaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.User == nil
}

func (a *API) checkLatestRelease(r *http.Request) releaseNotice {
	running := a.version
	if !updatecheck.CanCheck(running) {
		return releaseNotice{}
	}
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return releaseNotice{}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agent-marketplace-gui/"+a.version)
	resp, err := a.releaseClient.Do(req)
	if err != nil {
		return releaseNotice{}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return releaseNotice{}
	}
	var latest struct {
		TagName     string         `json:"tag_name"`
		Draft       *bool          `json:"draft"`
		Prerelease  *bool          `json:"prerelease"`
		PublishedAt time.Time      `json:"published_at"`
		HTMLURL     string         `json:"html_url"`
		Assets      []releaseAsset `json:"assets"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20))
	if err := decoder.Decode(&latest); err != nil {
		return releaseNotice{}
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return releaseNotice{}
	}
	if latest.Draft == nil || latest.Prerelease == nil || *latest.Draft || *latest.Prerelease || latest.PublishedAt.IsZero() || !isGitHubReleaseURL(latest.HTMLURL) {
		return releaseNotice{}
	}
	tag := normalizeReleaseVersion(latest.TagName)
	if tag == "" || semver.Prerelease(tag) != "" {
		return releaseNotice{}
	}
	if _, ok := updatecheck.Notice(running, updatecheck.Release{Tag: tag, PublishedAt: latest.PublishedAt, URL: latest.HTMLURL}); !ok {
		return releaseNotice{}
	}
	notice := releaseNotice{LatestVersion: tag, ReleasePageURL: latest.HTMLURL}
	assetName := guiReleaseAssetName(a.packageFormat, runtime.GOARCH, latest.TagName)
	if assetName == "" {
		return notice
	}
	var assetURL, checksumsURL string
	for _, asset := range latest.Assets {
		switch asset.Name {
		case assetName:
			assetURL = asset.BrowserDownloadURL
		case "SHA256SUMS":
			checksumsURL = asset.BrowserDownloadURL
		}
	}
	if isGitHubReleaseURL(assetURL) && isGitHubReleaseURL(checksumsURL) {
		notice.AssetURL = assetURL
		notice.ChecksumsURL = checksumsURL
	}
	return notice
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
	if !updatecheck.Enabled(os.Getenv) {
		writeReleaseNotice(w, releaseNotice{})
		return
	}
	enabled, err := config.UpdateCheckEnabled()
	if err != nil || !enabled {
		writeReleaseNotice(w, releaseNotice{})
		return
	}
	if !updatecheck.CanCheck(a.version) {
		writeReleaseNotice(w, releaseNotice{})
		return
	}
	a.releaseOnce.Do(func() {
		a.releaseNotice = a.checkLatestRelease(r)
	})
	writeReleaseNotice(w, a.releaseNotice)
}

func writeReleaseNotice(w http.ResponseWriter, notice releaseNotice) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(notice)
}
