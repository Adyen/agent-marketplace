// Package updatecheck tells users when a newer agent-marketplace release exists.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

const (
	// EnvDisable suppresses the release lookup and notice when set to a non-empty value.
	EnvDisable = "AGENT_MARKETPLACE_NO_UPDATE_CHECK"
	// DefaultEndpoint returns the latest published, non-prerelease release.
	DefaultEndpoint = "https://api.github.com/repos/Adyen/agent-marketplace/releases/latest"
	// StateFileName is the lookup cache under the Agent Marketplace state root.
	StateFileName = "update-check.json"

	interval       = 24 * time.Hour
	requestTimeout = 2 * time.Second
	maxBodyBytes   = 1 << 20
	upgradeCommand = "go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest"
)

// Release is the latest published release as recorded in the state file.
type Release struct {
	Tag         string    `json:"tag"`
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"url"`
}

// apiRelease holds the only GitHub response fields this package reads.
type apiRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// state is the on-disk cache.
type state struct {
	CheckedAt  time.Time `json:"checked_at"`
	Latest     *Release  `json:"latest"`
	NotifiedAt time.Time `json:"notified_at"`
}

type buildKind int

const (
	buildUnchecked buildKind = iota
	buildRelease
	buildPseudo
)

// build is the classified running version. commit is set for pseudo-versions.
type build struct {
	kind   buildKind
	commit time.Time
}

// Enabled reports whether the environment allows a release lookup and notice.
func Enabled(getenv func(string) string) bool {
	return getenv(EnvDisable) == "" && getenv("CI") == ""
}

// classify decides whether version can be compared with a release. Pseudo-versions
// are checked first because they are also valid semantic-version prereleases.
func classify(version string) build {
	if module.IsPseudoVersion(version) {
		commit, err := module.PseudoVersionTime(version)
		if err != nil {
			return build{}
		}
		return build{kind: buildPseudo, commit: commit}
	}
	if semver.IsValid(version) && semver.Canonical(version) == version && semver.Prerelease(version) == "" {
		return build{kind: buildRelease}
	}
	return build{}
}

// CanCheck reports whether version uses a release or Go pseudo-version build kind.
func CanCheck(version string) bool { return classify(version).kind != buildUnchecked }

// Notice returns the notice line when current is outdated relative to latest.
func Notice(current string, latest Release) (string, bool) {
	if !semver.IsValid(latest.Tag) {
		return "", false
	}
	b := classify(current)
	switch b.kind {
	case buildRelease:
		if semver.Compare(latest.Tag, current) <= 0 {
			return "", false
		}
		return fmt.Sprintf("info: agent-marketplace %s is available (you have %s): %s", latest.Tag, current, latest.URL), true
	case buildPseudo:
		if !latest.PublishedAt.After(b.commit) {
			return "", false
		}
		return fmt.Sprintf("info: agent-marketplace %s was released after this build (%s); run: %s", latest.Tag, b.commit.Format(time.DateOnly), upgradeCommand), true
	}
	return "", false
}

// Checker performs at most one release lookup per interval and reports the notice.
type Checker struct {
	Version   string
	StatePath string
	Endpoint  string
	Client    *http.Client
	Now       func() time.Time
}

// New returns a Checker that queries the public GitHub API.
func New(version, statePath string) *Checker {
	return &Checker{
		Version:   version,
		StatePath: statePath,
		Endpoint:  DefaultEndpoint,
		Client:    &http.Client{Timeout: requestTimeout},
		Now:       time.Now,
	}
}

type lookupResult struct {
	release Release
	err     error
}

// Run is one command's check. Start computes the notice from the cache alone;
// Finish waits for the lookup and persists the outcome.
type Run struct {
	checker   *Checker
	state     state
	pending   string
	attempted bool
	result    chan lookupResult
}

// Start reads the cache, computes any pending notice, and starts a lookup in the
// background when the cached attempt is at least a day old. It never waits on the network.
func (c *Checker) Start(ctx context.Context) *Run {
	run := &Run{checker: c, state: loadState(c.StatePath)}
	if !CanCheck(c.Version) {
		return run
	}
	now := c.Now()
	if run.state.Latest != nil && now.Sub(run.state.NotifiedAt) >= interval {
		run.pending, _ = Notice(c.Version, *run.state.Latest)
	}
	if now.Sub(run.state.CheckedAt) >= interval {
		run.attempted = true
		run.result = make(chan lookupResult, 1)
		go func() {
			release, err := fetchLatest(ctx, c)
			run.result <- lookupResult{release: release, err: err}
		}()
	}
	return run
}

// Finish waits up to maxWait for the lookup, persists the attempt and any shown
// notice, and returns the notice to print, or "" when there is none.
func (r *Run) Finish(maxWait time.Duration) string {
	if r == nil || r.checker == nil {
		return ""
	}
	now := r.checker.Now()
	changed := false
	if r.attempted {
		r.state.CheckedAt = now
		changed = true
		timer := time.NewTimer(maxWait)
		defer timer.Stop()
		select {
		case res := <-r.result:
			if res.err != nil {
				slog.Debug("upgrade check failed", "error", res.err)
			} else {
				r.state.Latest = &res.release
			}
		case <-timer.C:
			slog.Debug("upgrade check still running after bound", "bound", maxWait)
		}
	}
	if r.pending != "" {
		r.state.NotifiedAt = now
		changed = true
	}
	if changed {
		if err := saveState(r.checker.StatePath, r.state); err != nil {
			slog.Debug("upgrade check state not saved", "error", err)
		}
	}
	return r.pending
}

func fetchLatest(ctx context.Context, c *Checker) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return Release{}, fmt.Errorf("build release lookup: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agent-marketplace/"+c.Version)
	resp, err := c.Client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("release lookup: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("release lookup: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return Release{}, fmt.Errorf("release lookup: read body: %w", err)
	}
	if len(body) > maxBodyBytes {
		return Release{}, errors.New("release lookup: response exceeds size limit")
	}
	var wire apiRelease
	if err := json.Unmarshal(body, &wire); err != nil {
		return Release{}, fmt.Errorf("release lookup: decode: %w", err)
	}
	if !semver.IsValid(wire.TagName) || wire.PublishedAt.IsZero() || wire.HTMLURL == "" {
		return Release{}, errors.New("release lookup: incomplete release")
	}
	return Release{Tag: wire.TagName, PublishedAt: wire.PublishedAt, URL: wire.HTMLURL}, nil
}

// loadState treats a missing or corrupt cache as empty.
func loadState(path string) state {
	data, err := os.ReadFile(path)
	if err != nil {
		return state{}
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return state{}
	}
	return st
}

// saveState writes the cache atomically: a private temporary file renamed over the target.
func saveState(path string, st state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, StateFileName+".*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
