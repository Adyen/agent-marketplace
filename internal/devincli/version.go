// Package devincli detects a locally installed Devin CLI and decides whether
// it satisfies the compatibility floor.
//
// It implements the detection half of work package D11 of
// docs/plans/2026-08-03-devin-first-class-harness.md:
//
//	"Sync does not require a Devin binary. `doctor` detects an installed binary
//	and warns when its version is below 3000.3." (plan, "Roots")
//
//	"`doctor` detects `devin`, prints the installed version, reports
//	compatibility with 3000.3+, and recommends selecting the harness when
//	installed but unconfigured." (plan, "Rollout UX")
//
// Two properties are load-bearing and are enforced structurally rather than by
// convention:
//
//  1. Detection can never fail a command. [Detect] returns a [Probe] and NO
//     error. Every failure mode — no binary on PATH, a non-zero exit, a probe
//     that hangs, output with no recognizable version — collapses into a
//     [Status] value plus an advisory Probe.Err. There is no code path in which
//     a misbehaving third-party binary can make `doctor` (or anything else)
//     return an error, and no code path in which a missing binary blocks sync.
//  2. Nothing the probed binary prints reaches a log or the user verbatim
//     beyond a bounded, single-line, whitespace-collapsed excerpt of its own
//     version banner. The probe reads at most [maxProbeOutput] bytes, so a
//     binary that streams forever cannot exhaust memory, and it never reads,
//     logs, or reports any user file.
package devincli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Binary is the executable name looked up on PATH.
const Binary = "devin"

// FloorMajor and FloorMinor are the compatibility floor from the plan's header:
// "**Compatibility floor:** Devin CLI 3000.3". A patch level is deliberately not
// part of the floor: the evidence snapshot was captured on 3000.3.27, but the
// plan's contract is stated as "3000.3+".
const (
	FloorMajor = 3000
	FloorMinor = 3
)

// FloorString renders the compatibility floor for user-facing output.
const FloorString = "3000.3"

// defaultTimeout bounds the probe. `devin --version` is a local process that
// prints one line; anything slower than this is a misbehaving binary, and the
// plan requires doctor to keep working regardless.
const defaultTimeout = 2 * time.Second

// maxProbeOutput bounds how much of the probe's output is read.
const maxProbeOutput = 4 << 10

// waitDelay bounds how long the probe waits for output pipes after the process
// has been killed; see runVersionCommand.
const waitDelay = 250 * time.Millisecond

// Status is the outcome of one detection attempt.
type Status string

const (
	// StatusNotInstalled means no `devin` executable was found on PATH. This is
	// not a problem: "Sync does not require a Devin binary."
	StatusNotInstalled Status = "not-installed"
	// StatusUnknown means a binary was found but its version could not be
	// established: it exited non-zero, it hung and was killed, or its output
	// contained no recognizable version. The floor is NOT assumed either way.
	StatusUnknown Status = "unknown"
	// StatusUnsupported means a version was parsed and it is below the floor.
	StatusUnsupported Status = "unsupported"
	// StatusSupported means a version was parsed and it is at or above the floor.
	StatusSupported Status = "supported"
)

// Version is a parsed Devin version.
type Version struct {
	Major int
	Minor int
	// Patch is -1 when the binary reported only major.minor.
	Patch int
}

// String renders the version as the binary reported it.
func (v Version) String() string {
	if v.Patch < 0 {
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// AtLeastFloor reports whether the version satisfies the 3000.3 floor.
func (v Version) AtLeastFloor() bool {
	if v.Major != FloorMajor {
		return v.Major > FloorMajor
	}
	return v.Minor >= FloorMinor
}

// Probe is the result of one detection attempt. It is always usable: a zero
// Probe means "not installed", which is the safe reading everywhere.
type Probe struct {
	Status Status
	// Path is the resolved executable path, empty when nothing was found. It is
	// a PATH lookup result, never content read out of a user's files.
	Path string
	// Version is set only for StatusUnsupported and StatusSupported.
	Version Version
	// Reported is a bounded, single-line excerpt of the binary's own version
	// banner, kept so an unparseable version can be shown to the user without
	// them having to re-run the probe. It is empty when nothing was read.
	Reported string
	// Err is ADVISORY. It explains a StatusUnknown and is never returned to a
	// caller as a command failure.
	Err error
}

// Installed reports whether an executable was found, regardless of version.
func (p Probe) Installed() bool { return p.Status != StatusNotInstalled }

// Option customizes detection. Production code passes none.
type Option func(*config)

type config struct {
	timeout  time.Duration
	lookPath func(string) (string, error)
	run      func(ctx context.Context, path string) ([]byte, error)
}

// WithTimeout overrides the probe timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// WithLookPath overrides PATH resolution.
func WithLookPath(fn func(string) (string, error)) Option {
	return func(c *config) { c.lookPath = fn }
}

// WithRunner overrides how the version command is executed.
func WithRunner(fn func(ctx context.Context, path string) ([]byte, error)) Option {
	return func(c *config) { c.run = fn }
}

// versionPattern matches the first dotted version token in a banner such as
// `devin 3000.3.27 (0becb483)` (docs/devin-cli-harness-spec-3000.3.md,
// "Installed binary"). It requires at least major.minor so a build hash or a
// bare year cannot be misread as a version.
var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// Detect probes the locally installed Devin CLI.
//
// It never returns an error. Every failure is a Status, which is what lets
// `doctor` report the outcome and still exit 0, and what keeps `sync`
// independent of the binary entirely.
func Detect(ctx context.Context, opts ...Option) Probe {
	cfg := config{
		timeout:  defaultTimeout,
		lookPath: exec.LookPath,
		run:      runVersionCommand,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	path, err := cfg.lookPath(Binary)
	if err != nil || strings.TrimSpace(path) == "" {
		slog.Debug("devin cli not detected", "binary", Binary, "status", string(StatusNotInstalled))
		return Probe{Status: StatusNotInstalled}
	}

	probeCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	out, runErr := cfg.run(probeCtx, path)
	reported := excerpt(out)
	if runErr != nil {
		// A non-zero exit, a kill after the timeout, or a start failure. The
		// binary exists, so it is NOT reported as missing; the version is simply
		// unknown and the floor is not assumed in either direction.
		probe := Probe{Status: StatusUnknown, Path: path, Reported: reported, Err: runErr}
		slog.Warn("devin cli version probe failed; compatibility is unknown",
			"binary_path", path, "status", string(StatusUnknown), "error", runErr,
			"timed_out", errors.Is(probeCtx.Err(), context.DeadlineExceeded))
		return probe
	}

	version, ok := parseVersion(reported)
	if !ok {
		slog.Warn("devin cli version output was not parseable; compatibility is unknown",
			"binary_path", path, "status", string(StatusUnknown))
		return Probe{
			Status:   StatusUnknown,
			Path:     path,
			Reported: reported,
			Err:      fmt.Errorf("no version found in %q output", Binary),
		}
	}

	status := StatusUnsupported
	if version.AtLeastFloor() {
		status = StatusSupported
	}
	slog.Info("devin cli detected",
		"binary_path", path, "version", version.String(), "floor", FloorString, "status", string(status))
	return Probe{Status: status, Path: path, Version: version, Reported: reported}
}

// runVersionCommand executes `devin --version`, the probe the evidence snapshot
// prescribes: "Print and assert `devin --version` is at least 3000.3"
// (docs/devin-cli-harness-spec-3000.3.md, "Local probe suite requirements").
//
// Output is read through a bounded reader so a binary that never stops writing
// cannot consume memory, and stderr is folded in because some CLIs print their
// banner there.
func runVersionCommand(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, "--version")
	// Nothing is piped in: a probe must never block waiting for input, and it
	// must never be handed anything from the user's terminal.
	cmd.Stdin = nil
	// WaitDelay is what actually makes the timeout binding. Killing the probe
	// on context cancellation is not enough: if the probe spawned a child that
	// inherited the output pipe, Cmd.Run blocks reading that pipe until the
	// GRANDCHILD exits, so a `devin` wrapper script that shells out to a
	// long-running process would hang doctor for as long as it liked.
	// WaitDelay closes the pipes shortly after cancellation and returns.
	cmd.WaitDelay = waitDelay
	var buf bytes.Buffer
	limited := &limitedWriter{w: &buf, remaining: maxProbeOutput}
	cmd.Stdout = limited
	cmd.Stderr = limited
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return buf.Bytes(), fmt.Errorf("%s --version did not complete within the probe timeout: %w", Binary, ctxErr)
	}
	if err != nil {
		return buf.Bytes(), fmt.Errorf("%s --version failed: %w", Binary, err)
	}
	return buf.Bytes(), nil
}

// limitedWriter discards everything past a byte budget instead of failing, so a
// chatty binary produces a truncated banner rather than a probe error.
type limitedWriter struct {
	w         io.Writer
	remaining int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	total := len(p)
	if l.remaining <= 0 {
		return total, nil
	}
	if len(p) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.w.Write(p)
	l.remaining -= n
	if err != nil {
		return n, err
	}
	return total, nil
}

// excerpt renders probe output as one bounded, whitespace-collapsed line.
func excerpt(out []byte) string {
	if len(out) > maxProbeOutput {
		out = out[:maxProbeOutput]
	}
	line := strings.Join(strings.Fields(string(out)), " ")
	const maxRunes = 120
	runes := []rune(line)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return line
}

// parseVersion extracts the first dotted version token from a banner.
func parseVersion(reported string) (Version, bool) {
	match := versionPattern.FindStringSubmatch(reported)
	if match == nil {
		return Version{}, false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return Version{}, false
	}
	minor, err := strconv.Atoi(match[2])
	if err != nil {
		return Version{}, false
	}
	patch := -1
	if match[3] != "" {
		parsed, err := strconv.Atoi(match[3])
		if err != nil {
			return Version{}, false
		}
		patch = parsed
	}
	return Version{Major: major, Minor: minor, Patch: patch}, true
}
