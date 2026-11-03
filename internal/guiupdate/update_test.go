package guiupdate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type updateDownloads struct {
	archive   []byte
	checksums []byte
	requests  []string
}

func (d *updateDownloads) RoundTrip(r *http.Request) (*http.Response, error) {
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

type fakeMacPlatform struct {
	appPath       string
	translocated  bool
	onDiskImage   bool
	writable      bool
	info          map[string]BundleInfo
	renameCalls   int
	failRenameAt  int
	renameStarted chan struct{}
	allowRename   chan struct{}
	relaunchCalls int
	candidateInfo BundleInfo
	signatureErr  error
}

func (p *fakeMacPlatform) AppPath() (string, error) { return p.appPath, nil }
func (p *fakeMacPlatform) IsTranslocated(string) (bool, error) {
	return p.translocated, nil
}
func (p *fakeMacPlatform) IsOnDiskImage(string) (bool, error)  { return p.onDiskImage, nil }
func (p *fakeMacPlatform) ParentWritable(string) (bool, error) { return p.writable, nil }
func (p *fakeMacPlatform) ReadBundleInfo(path string) (BundleInfo, error) {
	info, ok := p.info[path]
	if !ok {
		return BundleInfo{}, os.ErrNotExist
	}
	return info, nil
}
func (p *fakeMacPlatform) VerifyCodeSignature(context.Context, string) error { return p.signatureErr }
func (p *fakeMacPlatform) ExtractArchive(_ context.Context, _ string, destination string) (string, error) {
	bundle := filepath.Join(destination, "Agent Marketplace.app")
	if err := os.Mkdir(bundle, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(bundle, "version"), []byte("new"), 0o644); err != nil {
		return "", err
	}
	p.info[bundle] = p.candidateInfo
	return bundle, nil
}
func (p *fakeMacPlatform) Rename(oldPath, newPath string) error {
	p.renameCalls++
	if p.renameCalls == 1 && p.renameStarted != nil {
		close(p.renameStarted)
		select {
		case <-p.allowRename:
		case <-time.After(3 * time.Second):
			return errors.New("rename timed out")
		}
	}
	if p.renameCalls == p.failRenameAt {
		return errors.New("rename rejected")
	}
	return os.Rename(oldPath, newPath)
}
func (p *fakeMacPlatform) RelaunchAfterExit(context.Context, string) error {
	p.relaunchCalls++
	return nil
}
func (p *fakeMacPlatform) RemoveAll(path string) error { return os.RemoveAll(path) }

type fakeLinuxPlatform struct {
	downloadDir string
	opened      []string
	openErr     error
}

func (p *fakeLinuxPlatform) DownloadDir() (string, error) { return p.downloadDir, nil }
func (p *fakeLinuxPlatform) OpenPackage(_ context.Context, path string) error {
	p.opened = append(p.opened, path)
	return p.openErr
}

type updateFixture struct {
	service   *Service
	downloads *updateDownloads
	mac       *fakeMacPlatform
	app       string
}

func newMacFixture(t *testing.T) updateFixture {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, "Agent Marketplace.app")
	if err := os.Mkdir(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "version"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := []byte("verified-archive")
	digest := sha256.Sum256(archive)
	asset := "agent-marketplace-gui-v1.3.0-macos-universal.zip"
	downloads := &updateDownloads{
		archive:   archive,
		checksums: []byte(fmt.Sprintf("%x  %s\n", digest, asset)),
	}
	mac := &fakeMacPlatform{
		appPath:  app,
		writable: true,
		candidateInfo: BundleInfo{
			Identifier:   "io.agent-marketplace.gui",
			Version:      "1.3.0",
			BuildVersion: "1.3.0",
		},
		info: map[string]BundleInfo{
			app: {Identifier: "io.agent-marketplace.gui", Version: "1.2.0", BuildVersion: "1.2.0"},
		},
	}
	service := New(Config{
		Format:       "macos-app",
		Architecture: runtime.GOARCH,
		Client:       &http.Client{Transport: downloads},
		Mac:          mac,
	})
	return updateFixture{service: service, downloads: downloads, mac: mac, app: app}
}

func updateRelease(format, arch string) Release {
	tag := "v1.3.0"
	base := "https://github.com/Adyen/agent-marketplace/releases/download/" + tag + "/"
	var asset string
	switch format {
	case "macos-app":
		asset = "agent-marketplace-gui-" + tag + "-macos-universal.zip"
	case "rpm", "deb":
		asset = "agent-marketplace-gui-" + tag + "-linux-" + arch + "." + format
	}
	return Release{
		Version:        tag,
		AssetURL:       base + asset,
		ChecksumsURL:   base + "SHA256SUMS",
		ReleasePageURL: "https://github.com/Adyen/agent-marketplace/releases/tag/" + tag,
		DMGURL:         base + "agent-marketplace-gui-" + tag + "-macos-universal.dmg",
	}
}

func waitUpdate(t *testing.T, service *Service) Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := service.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	return service.Status()
}

func readAppVersion(t *testing.T, app string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(app, "version"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// spec: gui-shell/user-updates-the-app-in-place
func TestMacUpdateReplacesAppAndCleansBackupOnNextStart(t *testing.T) {
	fixture := newMacFixture(t)
	if got := fixture.service.Start(updateRelease("macos-app", runtime.GOARCH)).Phase; got != PhaseDownloading {
		t.Fatalf("start phase = %q, want downloading", got)
	}
	if got := waitUpdate(t, fixture.service).Phase; got != PhaseReady {
		t.Fatalf("prepared phase = %q, want ready", got)
	}
	if got := readAppVersion(t, fixture.app); got != "old" {
		t.Fatalf("running app changed before confirmation: %q", got)
	}

	result, err := fixture.service.Confirm()
	if err != nil {
		t.Fatal(err)
	}
	if result.Phase != PhaseComplete || result.RestartRequired {
		t.Fatalf("confirmed update = %+v, want complete with automatic relaunch", result)
	}
	if got := readAppVersion(t, fixture.app); got != "new" {
		t.Fatalf("running app version = %q, want new", got)
	}
	if fixture.mac.relaunchCalls != 1 {
		t.Fatalf("relaunch calls = %d, want 1", fixture.mac.relaunchCalls)
	}

	backup := fixture.app + BackupSuffix
	if got := readAppVersion(t, backup); got != "old" {
		t.Fatalf("backup version = %q, want old", got)
	}
	if err := fixture.service.CleanupBackup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup remains after startup cleanup: %v", err)
	}
}

// spec: gui-shell/corrupted-update-download-is-rejected
func TestMacUpdateRejectsCorruptOrUnexpectedPayloads(t *testing.T) {
	cases := []struct {
		name       string
		noDownload bool
		configure  func(*updateFixture, *Release)
	}{
		{"digest mismatch", false, func(f *updateFixture, _ *Release) { f.downloads.archive = []byte("tampered") }},
		{"wrong bundle identifier", false, func(f *updateFixture, _ *Release) { f.mac.candidateInfo.Identifier = "other.bundle" }},
		{"wrong version", false, func(f *updateFixture, _ *Release) {
			f.mac.candidateInfo.Version, f.mac.candidateInfo.BuildVersion = "1.4.0", "1.4.0"
		}},
		{"wrong build version", false, func(f *updateFixture, _ *Release) { f.mac.candidateInfo.BuildVersion = "1.4.0" }},
		{"invalid code signature", false, func(f *updateFixture, _ *Release) { f.mac.signatureErr = errors.New("signature invalid") }},
		{"missing checksum entry", false, func(f *updateFixture, _ *Release) {
			f.downloads.checksums = []byte(strings.Repeat("0", 64) + "  another.zip\n")
		}},
		{"missing asset", true, func(_ *updateFixture, r *Release) { r.AssetURL = "" }},
		{"missing checksums URL", true, func(_ *updateFixture, r *Release) { r.ChecksumsURL = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newMacFixture(t)
			release := updateRelease("macos-app", runtime.GOARCH)
			tc.configure(&fixture, &release)
			fixture.service.Start(release)
			if got := waitUpdate(t, fixture.service).Phase; got != PhaseFailed {
				t.Fatalf("phase = %q, want failed", got)
			}
			if got := readAppVersion(t, fixture.app); got != "old" {
				t.Fatalf("running app version = %q, want old", got)
			}
			assertNoUpdateResidue(t, filepath.Dir(fixture.app), fixture.app)
			if tc.noDownload && len(fixture.downloads.requests) != 0 {
				t.Fatalf("download requests = %v, want none", fixture.downloads.requests)
			}
		})
	}
}

func TestMacUpdateCancellationAndSwapFailurePreserveRunningApp(t *testing.T) {
	t.Run("cancel before confirmation", func(t *testing.T) {
		fixture := newMacFixture(t)
		fixture.service.Start(updateRelease("macos-app", runtime.GOARCH))
		waitUpdate(t, fixture.service)
		result := fixture.service.Cancel()
		if result.Phase != PhaseCancelled {
			t.Fatalf("phase = %q, want cancelled", result.Phase)
		}
		if got := readAppVersion(t, fixture.app); got != "old" {
			t.Fatalf("running app version = %q, want old", got)
		}
		assertNoUpdateResidue(t, filepath.Dir(fixture.app), fixture.app)
	})

	t.Run("failed second rename restores backup", func(t *testing.T) {
		fixture := newMacFixture(t)
		fixture.mac.failRenameAt = 2
		fixture.service.Start(updateRelease("macos-app", runtime.GOARCH))
		waitUpdate(t, fixture.service)
		result, err := fixture.service.Confirm()
		if err == nil || result.Phase != PhaseFailed {
			t.Fatalf("confirm = %+v, %v; want failed result and error", result, err)
		}
		if got := readAppVersion(t, fixture.app); got != "old" {
			t.Fatalf("running app version = %q, want old after restore", got)
		}
		assertNoUpdateResidue(t, filepath.Dir(fixture.app), fixture.app)
	})
}

// spec: gui-shell/user-updates-the-app-in-place
func TestCancelDoesNotInterruptConfirmedMacUpdate(t *testing.T) {
	fixture := newMacFixture(t)
	fixture.mac.renameStarted = make(chan struct{})
	fixture.mac.allowRename = make(chan struct{})
	fixture.service.Start(updateRelease("macos-app", runtime.GOARCH))
	waitUpdate(t, fixture.service)

	confirmed := make(chan error, 1)
	go func() {
		_, err := fixture.service.Confirm()
		confirmed <- err
	}()
	select {
	case <-fixture.mac.renameStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("confirm did not start app replacement")
	}

	cancelled := fixture.service.Cancel()
	close(fixture.mac.allowRename)
	confirmErr := <-confirmed
	if cancelled.Phase != PhaseSwapping {
		t.Fatalf("cancel phase = %q, want swapping", cancelled.Phase)
	}
	if status := fixture.service.Status(); confirmErr != nil || status.Phase != PhaseComplete {
		t.Fatalf("confirm status = %+v, %v; want complete", status, confirmErr)
	}
	if got := readAppVersion(t, fixture.app); got != "new" {
		t.Fatalf("running app version = %q, want new", got)
	}
}

// spec: gui-shell/ineligible-location-falls-back-to-a-browser-download
func TestMacIneligibleLocationFallsBackToResolvedDMG(t *testing.T) {
	for _, tc := range []struct {
		name         string
		translocated bool
		onDiskImage  bool
		writable     bool
		moveToApps   bool
	}{
		{name: "App Translocation", translocated: true, writable: true, moveToApps: true},
		{name: "mounted disk image", onDiskImage: true, writable: true, moveToApps: true},
		{name: "unwritable parent", writable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newMacFixture(t)
			fixture.mac.translocated = tc.translocated
			fixture.mac.onDiskImage = tc.onDiskImage
			fixture.mac.writable = tc.writable
			fixture.service.Start(updateRelease("macos-app", runtime.GOARCH))
			result := waitUpdate(t, fixture.service)
			if result.Phase != PhaseFallback || result.BrowserURL != updateRelease("macos-app", runtime.GOARCH).DMGURL {
				t.Fatalf("fallback = %+v", result)
			}
			if result.MoveToApplications != tc.moveToApps {
				t.Fatalf("MoveToApplications = %v, want %v", result.MoveToApplications, tc.moveToApps)
			}
			if len(fixture.downloads.requests) != 0 {
				t.Fatalf("download requests = %v, want none", fixture.downloads.requests)
			}
			if got := readAppVersion(t, fixture.app); got != "old" {
				t.Fatalf("running app version = %q, want old", got)
			}
		})
	}
}

// spec: gui-shell/user-hands-the-verified-package-to-the-software-installer
func TestLinuxVerifiedPackageHandoff(t *testing.T) {
	archive := []byte("verified-package")
	digest := sha256.Sum256(archive)
	for _, format := range []string{"rpm", "deb"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			asset := fmt.Sprintf("agent-marketplace-gui-v1.3.0-linux-%s.%s", runtime.GOARCH, format)
			if err := os.WriteFile(filepath.Join(dir, asset), []byte("previous package"), 0o644); err != nil {
				t.Fatal(err)
			}
			downloads := &updateDownloads{
				archive:   archive,
				checksums: []byte(fmt.Sprintf("%x  %s\n", digest, asset)),
			}
			platform := &fakeLinuxPlatform{downloadDir: dir}
			service := New(Config{
				Format:       format,
				Architecture: runtime.GOARCH,
				Client:       &http.Client{Transport: downloads},
				Linux:        platform,
			})
			service.Start(updateRelease(format, runtime.GOARCH))
			result := waitUpdate(t, service)
			if result.Phase != PhaseComplete || !result.RestartRequired {
				t.Fatalf("handoff = %+v, want complete and restart required", result)
			}
			if len(platform.opened) != 1 || platform.opened[0] != result.SavedPath {
				t.Fatalf("opened = %v, saved path = %q", platform.opened, result.SavedPath)
			}
			if got, err := os.ReadFile(result.SavedPath); err != nil || string(got) != string(archive) {
				t.Fatalf("saved package = %q, %v", got, err)
			}
		})
	}
}

// spec: gui-shell/unverified-linux-package-is-not-opened
func TestLinuxUnverifiedPackageIsDeletedAndNeverOpened(t *testing.T) {
	dir := t.TempDir()
	archive := []byte("tampered-package")
	asset := fmt.Sprintf("agent-marketplace-gui-v1.3.0-linux-%s.deb", runtime.GOARCH)
	downloads := &updateDownloads{
		archive:   archive,
		checksums: []byte(strings.Repeat("0", 64) + "  " + asset + "\n"),
	}
	platform := &fakeLinuxPlatform{downloadDir: dir}
	service := New(Config{
		Format:       "deb",
		Architecture: runtime.GOARCH,
		Client:       &http.Client{Transport: downloads},
		Linux:        platform,
	})

	service.Start(updateRelease("deb", runtime.GOARCH))
	result := waitUpdate(t, service)
	if result.Phase != PhaseFailed {
		t.Fatalf("phase = %q, want failed", result.Phase)
	}
	if len(platform.opened) != 0 {
		t.Fatalf("opened = %v, want none", platform.opened)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unverified download remains: %v", entries)
	}
}

func TestLinuxOpenFailureKeepsVerifiedPackageAndReleasePage(t *testing.T) {
	dir := t.TempDir()
	archive := []byte("verified-package")
	digest := sha256.Sum256(archive)
	asset := fmt.Sprintf("agent-marketplace-gui-v1.3.0-linux-%s.rpm", runtime.GOARCH)
	downloads := &updateDownloads{
		archive:   archive,
		checksums: []byte(fmt.Sprintf("%x  %s\n", digest, asset)),
	}
	platform := &fakeLinuxPlatform{downloadDir: dir, openErr: errors.New("no desktop handler")}
	service := New(Config{
		Format:       "rpm",
		Architecture: runtime.GOARCH,
		Client:       &http.Client{Transport: downloads},
		Linux:        platform,
	})

	service.Start(updateRelease("rpm", runtime.GOARCH))
	result := waitUpdate(t, service)
	if result.Phase != PhaseFallback {
		t.Fatalf("phase = %q, want fallback", result.Phase)
	}
	if result.SavedPath == "" || result.ReleasePageURL != updateRelease("rpm", runtime.GOARCH).ReleasePageURL {
		t.Fatalf("fallback = %+v", result)
	}
	if got, err := os.ReadFile(result.SavedPath); err != nil || string(got) != string(archive) {
		t.Fatalf("verified package = %q, %v", got, err)
	}
}

func assertNoUpdateResidue(t *testing.T, parent, app string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(app) {
		t.Fatalf("update staging residue in %s: %v", parent, entries)
	}
}
