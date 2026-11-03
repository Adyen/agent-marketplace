package guiupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const BackupSuffix = ".agent-marketplace-update-backup"

const maxDownloadSize = 1 << 30

type Phase string

const (
	PhaseIdle        Phase = "idle"
	PhaseDownloading Phase = "downloading"
	PhaseVerifying   Phase = "verifying"
	PhaseReady       Phase = "ready"
	PhaseSwapping    Phase = "swapping"
	PhaseHandingOff  Phase = "handing-off"
	PhaseComplete    Phase = "complete"
	PhaseFallback    Phase = "fallback"
	PhaseFailed      Phase = "failed"
	PhaseCancelled   Phase = "cancelled"
)

type Status struct {
	Phase              Phase  `json:"phase"`
	Progress           int    `json:"progress"`
	Error              string `json:"error"`
	BrowserURL         string `json:"browserURL"`
	ReleasePageURL     string `json:"releasePageURL"`
	SavedPath          string `json:"savedPath"`
	MoveToApplications bool   `json:"moveToApplications"`
	RestartRequired    bool   `json:"restartRequired"`
}

type Release struct {
	Version        string
	ReleasePageURL string
	AssetURL       string
	ChecksumsURL   string
	DMGURL         string
}

type BundleInfo struct {
	Identifier   string
	Version      string
	BuildVersion string
}

type MacPlatform interface {
	AppPath() (string, error)
	IsTranslocated(appPath string) (bool, error)
	IsOnDiskImage(appPath string) (bool, error)
	ParentWritable(appPath string) (bool, error)
	ReadBundleInfo(appPath string) (BundleInfo, error)
	VerifyCodeSignature(ctx context.Context, appPath string) error
	ExtractArchive(ctx context.Context, archivePath, destination string) (string, error)
	Rename(oldPath, newPath string) error
	RelaunchAfterExit(ctx context.Context, appPath string) error
	RemoveAll(path string) error
}

type LinuxPlatform interface {
	DownloadDir() (string, error)
	OpenPackage(ctx context.Context, packagePath string) error
}

type Config struct {
	Format       string
	Architecture string
	Client       *http.Client
	Mac          MacPlatform
	Linux        LinuxPlatform
}

type Service struct {
	mu        sync.Mutex
	config    Config
	status    Status
	cancel    context.CancelFunc
	done      chan struct{}
	stageDir  string
	appPath   string
	stagedApp string
}

func New(config Config) *Service {
	if config.Architecture == "" {
		config.Architecture = runtime.GOARCH
	}
	if config.Client == nil {
		config.Client = downloadClient()
	}
	return &Service{config: config, status: Status{Phase: PhaseIdle}}
}

func downloadClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 || req.URL.Scheme != "https" || req.URL.User != nil {
				return http.ErrUseLastResponse
			}
			host := req.URL.Hostname()
			if host == "github.com" || host == "release-assets.githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com") {
				return nil
			}
			return http.ErrUseLastResponse
		},
	}
}

func (s *Service) Start(release Release) Status {
	s.mu.Lock()
	if updateActive(s.status.Phase) {
		status := s.status
		s.mu.Unlock()
		return status
	}
	oldCancel := s.cancel
	oldDone := s.done
	s.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if oldDone != nil {
		<-oldDone
	}
	s.removeStage()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.cancel = cancel
	s.done = done
	s.appPath = ""
	s.stagedApp = ""
	initial := Status{Phase: PhaseDownloading, Progress: 5}
	s.status = initial
	s.mu.Unlock()
	slog.Info("GUI update started", "format", s.config.Format, "version", release.Version)
	go func() {
		defer close(done)
		switch s.config.Format {
		case "macos-app":
			s.updateMac(ctx, release)
		case "rpm", "deb":
			s.updateLinux(ctx, release)
		default:
			s.finish(ctx, Status{Phase: PhaseFailed, Error: "this build has no update package format"})
		}
	}()
	return initial
}

func updateActive(phase Phase) bool {
	switch phase {
	case PhaseDownloading, PhaseVerifying, PhaseReady, PhaseSwapping, PhaseHandingOff:
		return true
	default:
		return false
	}
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Service) Wait(ctx context.Context) error {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) Confirm() (Status, error) {
	s.mu.Lock()
	if s.config.Format != "macos-app" || s.status.Phase != PhaseReady || s.config.Mac == nil {
		status := s.status
		s.mu.Unlock()
		return status, errors.New("no verified macOS update is ready")
	}
	appPath, stagedApp, stageDir := s.appPath, s.stagedApp, s.stageDir
	ctx := context.Background()
	s.status.Phase = PhaseSwapping
	s.status.Progress = 90
	s.mu.Unlock()

	backup := appPath + BackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		return s.failConfirm(stageDir, fmt.Errorf("update backup already exists at %s", backup))
	} else if !errors.Is(err, os.ErrNotExist) {
		return s.failConfirm(stageDir, fmt.Errorf("inspect update backup: %w", err))
	}
	if err := s.config.Mac.Rename(appPath, backup); err != nil {
		return s.failConfirm(stageDir, fmt.Errorf("back up running app: %w", err))
	}
	if err := s.config.Mac.Rename(stagedApp, appPath); err != nil {
		restoreErr := s.config.Mac.Rename(backup, appPath)
		if restoreErr != nil {
			return s.failConfirm(stageDir, fmt.Errorf("install update: %w; restore running app: %v", err, restoreErr))
		}
		return s.failConfirm(stageDir, fmt.Errorf("install update: %w", err))
	}
	if err := s.config.Mac.RelaunchAfterExit(ctx, appPath); err != nil {
		removeErr := s.config.Mac.RemoveAll(appPath)
		restoreErr := s.config.Mac.Rename(backup, appPath)
		if removeErr != nil || restoreErr != nil {
			return s.failConfirm(stageDir, fmt.Errorf("schedule updated app relaunch: %w; rollback failed: remove=%v restore=%v", err, removeErr, restoreErr))
		}
		return s.failConfirm(stageDir, fmt.Errorf("schedule updated app relaunch: %w", err))
	}
	if err := s.config.Mac.RemoveAll(stageDir); err != nil {
		slog.Warn("could not remove GUI update staging directory", "err", err)
	}
	s.mu.Lock()
	s.stageDir = ""
	s.stagedApp = ""
	s.status = Status{Phase: PhaseComplete, Progress: 100}
	status := s.status
	s.mu.Unlock()
	slog.Info("GUI app update replaced the running bundle")
	return status, nil
}

func (s *Service) failConfirm(stageDir string, err error) (Status, error) {
	if stageDir != "" {
		_ = s.config.Mac.RemoveAll(stageDir)
	}
	s.mu.Lock()
	s.stageDir = ""
	s.stagedApp = ""
	s.status = Status{Phase: PhaseFailed, Error: err.Error()}
	status := s.status
	s.mu.Unlock()
	return status, err
}

func (s *Service) Cancel() Status {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	if cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	s.removeStage()
	s.mu.Lock()
	s.status = Status{Phase: PhaseCancelled}
	status := s.status
	s.mu.Unlock()
	return status
}

func (s *Service) Close() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	if cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	s.removeStage()
}

func (s *Service) CleanupBackup() error {
	if s.config.Format != "macos-app" || s.config.Mac == nil {
		return nil
	}
	appPath, err := s.config.Mac.AppPath()
	if err != nil {
		return fmt.Errorf("resolve app path for update cleanup: %w", err)
	}
	backup := appPath + BackupSuffix
	if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect update backup: %w", err)
	}
	if err := s.config.Mac.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove prior update backup: %w", err)
	}
	return nil
}

func (s *Service) updateMac(ctx context.Context, release Release) {
	if s.config.Mac == nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: "macOS update backend is unavailable"})
		return
	}
	if err := validateRelease(release, s.config.Format, s.config.Architecture); err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: err.Error()})
		return
	}
	appPath, err := s.config.Mac.AppPath()
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("resolve running app: %w", err).Error()})
		return
	}
	translocated, err := s.config.Mac.IsTranslocated(appPath)
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("check app translocation: %w", err).Error()})
		return
	}
	onDiskImage, err := s.config.Mac.IsOnDiskImage(appPath)
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("check mounted disk image: %w", err).Error()})
		return
	}
	writable, err := s.config.Mac.ParentWritable(appPath)
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("check app parent permissions: %w", err).Error()})
		return
	}
	if translocated || onDiskImage || !writable {
		browserURL := release.DMGURL
		tag := releaseTag(release.AssetURL)
		dmgName := "agent-marketplace-gui-" + tag + "-macos-universal.dmg"
		if !validReleaseAssetURL(browserURL, tag, dmgName) {
			browserURL = release.ReleasePageURL
		}
		s.finish(ctx, Status{
			Phase:              PhaseFallback,
			Progress:           100,
			BrowserURL:         browserURL,
			ReleasePageURL:     release.ReleasePageURL,
			MoveToApplications: translocated || onDiskImage,
		})
		slog.Info("GUI update requires browser fallback", "move_to_applications", translocated || onDiskImage)
		return
	}
	currentInfo, err := s.config.Mac.ReadBundleInfo(appPath)
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("read running app metadata: %w", err).Error()})
		return
	}
	if currentInfo.Identifier == "" {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: "running app bundle identifier is missing"})
		return
	}
	parent := filepath.Dir(appPath)
	stageDir, err := os.MkdirTemp(parent, ".agent-marketplace-update-")
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("create update staging: %w", err).Error()})
		return
	}
	s.mu.Lock()
	s.appPath, s.stageDir = appPath, stageDir
	s.mu.Unlock()
	if err := s.prepareMac(ctx, release, stageDir, currentInfo); err != nil {
		s.removeStage()
		s.finish(ctx, Status{Phase: PhaseFailed, Error: err.Error()})
		return
	}
	if ctx.Err() != nil {
		s.removeStage()
		s.finish(ctx, Status{Phase: PhaseCancelled})
		return
	}
	s.mu.Lock()
	s.stagedApp = filepath.Join(stageDir, "Agent Marketplace.app")
	s.status = Status{Phase: PhaseReady, Progress: 100}
	s.mu.Unlock()
}

func (s *Service) prepareMac(ctx context.Context, release Release, stageDir string, current BundleInfo) error {
	assetName := filepath.Base(mustURLPath(release.AssetURL))
	archive, err := s.fetch(ctx, release.AssetURL)
	if err != nil {
		return fmt.Errorf("download update archive: %w", err)
	}
	s.setProgress(PhaseDownloading, 45)
	checksums, err := s.fetch(ctx, release.ChecksumsURL)
	if err != nil {
		return fmt.Errorf("download release checksums: %w", err)
	}
	if err := verifyChecksum(assetName, archive, checksums); err != nil {
		return fmt.Errorf("verify update archive: %w", err)
	}
	archivePath := filepath.Join(stageDir, assetName)
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		return fmt.Errorf("stage update archive: %w", err)
	}
	s.setProgress(PhaseVerifying, 70)
	stagedApp, err := s.config.Mac.ExtractArchive(ctx, archivePath, stageDir)
	if err != nil {
		return fmt.Errorf("extract update archive: %w", err)
	}
	if !pathWithin(stageDir, stagedApp) {
		return errors.New("extracted app is outside the update staging directory")
	}
	staged, err := s.config.Mac.ReadBundleInfo(stagedApp)
	if err != nil {
		return fmt.Errorf("read updated app metadata: %w", err)
	}
	if staged.Identifier != current.Identifier {
		return fmt.Errorf("updated app bundle identifier %q does not match running app %q", staged.Identifier, current.Identifier)
	}
	if staged.Version != strings.TrimPrefix(release.Version, "v") {
		return fmt.Errorf("updated app version %q does not match release %q", staged.Version, release.Version)
	}
	if staged.BuildVersion != strings.TrimPrefix(release.Version, "v") {
		return fmt.Errorf("updated app build version %q does not match release %q", staged.BuildVersion, release.Version)
	}
	if err := s.config.Mac.VerifyCodeSignature(ctx, stagedApp); err != nil {
		return fmt.Errorf("verify updated app code signature: %w", err)
	}
	return nil
}

func (s *Service) updateLinux(ctx context.Context, release Release) {
	if s.config.Linux == nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: "Linux update backend is unavailable"})
		return
	}
	if err := validateRelease(release, s.config.Format, s.config.Architecture); err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: err.Error()})
		return
	}
	dir, err := s.config.Linux.DownloadDir()
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("resolve download directory: %w", err).Error()})
		return
	}
	if !filepath.IsAbs(dir) {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: "download directory must be absolute"})
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("create download directory: %w", err).Error()})
		return
	}
	assetName := filepath.Base(mustURLPath(release.AssetURL))
	archive, err := s.fetch(ctx, release.AssetURL)
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("download update package: %w", err).Error()})
		return
	}
	s.setProgress(PhaseDownloading, 45)
	checksums, err := s.fetch(ctx, release.ChecksumsURL)
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("download release checksums: %w", err).Error()})
		return
	}
	s.setProgress(PhaseVerifying, 70)
	if err := verifyChecksum(assetName, archive, checksums); err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("verify update package: %w", err).Error()})
		return
	}
	if ctx.Err() != nil {
		s.finish(ctx, Status{Phase: PhaseCancelled})
		return
	}
	temp, err := os.CreateTemp(dir, ".agent-marketplace-gui-update-")
	if err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("create package download: %w", err).Error()})
		return
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()
	if _, err := temp.Write(archive); err != nil {
		_ = temp.Close()
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("save verified package: %w", err).Error()})
		return
	}
	if err := temp.Close(); err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("close verified package: %w", err).Error()})
		return
	}
	finalPath := filepath.Join(dir, assetName)
	if err := os.Link(tempName, finalPath); err != nil {
		s.finish(ctx, Status{Phase: PhaseFailed, Error: fmt.Errorf("publish verified package: %w", err).Error()})
		return
	}
	_ = os.Remove(tempName)
	s.setProgress(PhaseHandingOff, 90)
	if err := s.config.Linux.OpenPackage(ctx, finalPath); err != nil {
		s.finish(ctx, Status{
			Phase:          PhaseFallback,
			Progress:       100,
			Error:          err.Error(),
			ReleasePageURL: release.ReleasePageURL,
			SavedPath:      finalPath,
		})
		return
	}
	s.finish(ctx, Status{Phase: PhaseComplete, Progress: 100, SavedPath: finalPath, RestartRequired: true})
}

func (s *Service) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if !validGitHubURL(rawURL) {
		return nil, errors.New("download URL is not a trusted GitHub release URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.config.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownloadSize {
		return nil, errors.New("download exceeds size limit")
	}
	return data, nil
}

func validateRelease(release Release, format, arch string) error {
	if !validGitHubURL(release.ReleasePageURL) {
		return errors.New("release page URL is unavailable")
	}
	if !validGitHubURL(release.AssetURL) || !validGitHubURL(release.ChecksumsURL) {
		return errors.New("release package or checksums are unavailable")
	}
	tag := releaseTag(release.AssetURL)
	if tag == "" || strings.TrimPrefix(tag, "v") != strings.TrimPrefix(release.Version, "v") {
		return errors.New("release package does not match this version")
	}
	expected := ExpectedAssetName(tag, format, arch)
	if expected == "" || !validReleaseAssetURL(release.AssetURL, tag, expected) {
		return errors.New("release package does not match this build")
	}
	if !validReleaseAssetURL(release.ChecksumsURL, tag, "SHA256SUMS") {
		return errors.New("release checksum URL is invalid")
	}
	return nil
}

// ExpectedAssetName returns the release asset name this build's format and
// architecture update from, or empty when the build or tag cannot update.
func ExpectedAssetName(version, format, arch string) string {
	tag := strings.TrimPrefix(version, "v")
	if tag == "" || strings.ContainsAny(tag, "/\\") {
		return ""
	}
	switch format {
	case "macos-app":
		return "agent-marketplace-gui-" + version + "-macos-universal.zip"
	case "rpm", "deb":
		if arch != "amd64" && arch != "arm64" {
			return ""
		}
		return "agent-marketplace-gui-" + version + "-linux-" + arch + "." + format
	default:
		return ""
	}
}

func verifyChecksum(assetName string, archive, checksums []byte) error {
	var digest string
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != assetName {
			continue
		}
		if digest != "" {
			return errors.New("checksum file contains duplicate package entries")
		}
		if len(fields[0]) != sha256.Size*2 {
			return errors.New("checksum entry is malformed")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return errors.New("checksum entry is malformed")
		}
		digest = fields[0]
	}
	if digest == "" {
		return errors.New("checksum entry is missing")
	}
	sum := sha256.Sum256(archive)
	if !strings.EqualFold(digest, hex.EncodeToString(sum[:])) {
		return errors.New("package digest does not match SHA256SUMS")
	}
	return nil
}

// ValidReleaseAssetURL reports whether raw is the release download URL for
// the asset name at tag.
func ValidReleaseAssetURL(raw, tag, name string) bool { return validReleaseAssetURL(raw, tag, name) }

func validReleaseAssetURL(raw, tag, name string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && validGitHubURL(raw) && parsed.RawQuery == "" && parsed.Fragment == "" &&
		parsed.Path == path.Join("/Adyen/agent-marketplace/releases/download", tag, name)
}

func releaseTag(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return path.Base(path.Dir(parsed.Path))
}

func mustURLPath(raw string) string {
	parsed, _ := url.Parse(raw)
	return parsed.Path
}

func pathWithin(root, candidate string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *Service) setProgress(phase Phase, progress int) {
	s.mu.Lock()
	if s.status.Phase != PhaseCancelled && s.status.Phase != PhaseFailed {
		s.status.Phase = phase
		s.status.Progress = progress
	}
	s.mu.Unlock()
}

func (s *Service) finish(ctx context.Context, status Status) {
	if ctx.Err() != nil {
		status = Status{Phase: PhaseCancelled}
	}
	if status.Phase == PhaseFailed {
		slog.Warn("GUI update failed", "format", s.config.Format, "error", status.Error)
	}
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func (s *Service) removeStage() {
	s.mu.Lock()
	stageDir := s.stageDir
	s.stageDir = ""
	s.stagedApp = ""
	s.mu.Unlock()
	if stageDir == "" {
		return
	}
	if s.config.Mac != nil {
		_ = s.config.Mac.RemoveAll(stageDir)
		return
	}
	_ = os.RemoveAll(stageDir)
}
