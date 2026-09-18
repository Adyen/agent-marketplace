package analyze

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Runner is an out-of-process analyzer that invokes a configured command in an
// isolated, bounded, and redacted environment.
type Runner struct {
	id     string
	ver    string
	digest string
	cfg    RunnerConfig
}

// NewRunner creates an external analyzer runner from its static configuration.
//
// The constructor validates the supplied options and stores the validated hex
// digest. Command existence, regular-file, and executable checks are performed
// in Analyze so missing or broken executables produce an audit, not a
// construction-time panic.
func NewRunner(id, version string, cfg RunnerConfig) (*Runner, error) {
	if err := safeAnalyzerIdentifier(id); err != nil {
		return nil, fmt.Errorf("runner id: %w", err)
	}
	if version == "" {
		return nil, fmt.Errorf("runner version must not be empty")
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("runner command must not be empty")
	}
	if !filepath.IsAbs(cfg.Command) {
		return nil, fmt.Errorf("runner command must be an absolute path")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("runner timeout must be positive")
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 1 << 20 // 1 MiB default
	}
	digest, err := canonicalDigest(cfg.ImplementationDigest)
	if err != nil {
		return nil, fmt.Errorf("runner implementation_digest: %w", err)
	}
	if err := validateEnvNames(cfg.EnvPassthrough); err != nil {
		return nil, err
	}

	return &Runner{
		id:     id,
		ver:    version,
		digest: digest,
		cfg:    cfg,
	}, nil
}

func (r *Runner) ID() string               { return r.id }
func (r *Runner) Version() string          { return r.ver }
func (r *Runner) Kinds() []ir.ResourceKind { return r.cfg.Kinds }

func (r *Runner) Analyze(ctx context.Context, s Subject, cfg AnalyzerConfig) (Audit, error) {
	if ctx.Err() != nil {
		return Audit{}, ctx.Err()
	}
	if err := validateSubject(&s); err != nil {
		return Audit{}, err
	}
	if err := validateCommand(r.cfg.Command); err != nil {
		return Audit{}, err
	}

	// Isolated temp cwd; the verified executable copy is staged here so the
	// hashed bytes and the executed bytes are the same file, and cleanup is
	// owned by the run directory.
	tmpDir, err := os.MkdirTemp("", "am-analyzer-*")
	if err != nil {
		return Audit{}, fmt.Errorf("cannot create analyzer temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	execPath, err := r.stageVerifiedExecutable(tmpDir, maxRunnerExecutableBytes)
	if err != nil {
		return Audit{}, err
	}

	// Build the allowlisted, otherwise empty environment. The slice must stay
	// non-nil: a nil cmd.Env would silently inherit the entire parent
	// environment instead of the intended empty allowlist.
	env := make([]string, 0, len(r.cfg.EnvPassthrough))
	var envValues []string
	for _, name := range r.cfg.EnvPassthrough {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			env = append(env, name+"="+v)
			envValues = append(envValues, v)
		}
	}
	sort.Strings(env) // deterministic for tests

	req, err := r.buildRequest(s, cfg)
	if err != nil {
		return Audit{}, fmt.Errorf("cannot build runner request: %w", err)
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return Audit{}, fmt.Errorf("cannot marshal runner request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, execPath, r.cfg.Args...)
	// Kill the whole process group on timeout so a descendant holding the
	// inherited stdout/stderr pipes cannot keep Wait's pipe drain blocked
	// beyond the configured timeout.
	bindRunnerProcessGroup(cmd)
	// Bound Wait's post-kill pipe drain so a descendant that escaped the
	// process group (e.g. via setsid) and still holds inherited pipes cannot
	// block shutdown past the configured timeout.
	cmd.WaitDelay = runnerWaitDelay
	cmd.Dir = tmpDir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(reqJSON)

	stdoutW := &boundedWriter{max: r.cfg.MaxOutputBytes, buf: &bytes.Buffer{}}
	stderrW := &boundedWriter{max: r.cfg.MaxOutputBytes, buf: &bytes.Buffer{}}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Audit{}, fmt.Errorf("runner timed out after %s", r.cfg.Timeout)
		}
		if stdoutW.overflow {
			return Audit{}, fmt.Errorf("runner stdout exceeded %d bytes", r.cfg.MaxOutputBytes)
		}
		if stderrW.overflow {
			return Audit{}, fmt.Errorf("runner stderr exceeded %d bytes", r.cfg.MaxOutputBytes)
		}
		diag := redactStderr(stderrW.String(), envValues)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if diag != "" {
				return Audit{}, fmt.Errorf("runner exited with code %d: %s", exitErr.ExitCode(), diag)
			}
			return Audit{}, fmt.Errorf("runner exited with code %d", exitErr.ExitCode())
		}
		if diag != "" {
			return Audit{}, fmt.Errorf("runner failed: %s: %w", diag, err)
		}
		return Audit{}, fmt.Errorf("runner failed: %w", err)
	}

	if stdoutW.overflow {
		return Audit{}, fmt.Errorf("runner stdout exceeded %d bytes", r.cfg.MaxOutputBytes)
	}
	if stderrW.overflow {
		return Audit{}, fmt.Errorf("runner stderr exceeded %d bytes", r.cfg.MaxOutputBytes)
	}

	return r.parseResponse(stdoutW.Bytes(), envValues)
}

// runnerWaitDelay bounds how long Wait drains the child's inherited pipes
// after the process exits or is killed. A descendant that escaped the process
// group can keep the pipes open; without a bound Wait would block past the
// configured timeout.
const runnerWaitDelay = 2 * time.Second

// maxRunnerExecutableBytes is the strict documented maximum for staged
// executable input. The executable is trusted runner configuration, not
// analyzed content, so this fixed cap bounds it rather than the subject's
// MaxFileBytes input limit.
const maxRunnerExecutableBytes = 64 << 20 // 64 MiB

// stageVerifiedExecutable reads the configured command once, binds those exact
// bytes to the declared implementation_digest, and stages a private executable
// copy inside the isolated run directory. Executing the staged copy instead of
// re-opening cfg.Command closes the hash-then-exec TOCTOU window: replacing
// the original path after verification cannot change what runs.
//
// maxBytes bounds the input before any memory allocation. The stat size and a
// limited read both reject executables that exceed or grow past the bound, so
// a hostile or oversized binary cannot force an unbounded allocation.
func (r *Runner) stageVerifiedExecutable(dir string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = maxRunnerExecutableBytes
	}
	fi, err := os.Stat(r.cfg.Command)
	if err != nil {
		return "", fmt.Errorf("cannot stat runner executable: %w", err)
	}
	if fi.Size() > maxBytes {
		return "", fmt.Errorf("runner executable exceeds %d byte input limit", maxBytes)
	}
	f, err := os.Open(r.cfg.Command)
	if err != nil {
		return "", fmt.Errorf("cannot read runner executable: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("cannot read runner executable: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("runner executable exceeds %d byte input limit", maxBytes)
	}
	sum := sha256.Sum256(data)
	actualDigest := hex.EncodeToString(sum[:])
	if actualDigest != r.digest {
		return "", fmt.Errorf("implementation digest mismatch: expected sha256:%s, got sha256:%s", r.digest, actualDigest)
	}
	path := filepath.Join(dir, "runner-executable")
	if err := os.WriteFile(path, data, fi.Mode().Perm()|0o700); err != nil {
		return "", fmt.Errorf("cannot stage runner executable: %w", err)
	}
	// WriteFile perm passes through the umask; chmod restores the source
	// executable bits so a restrictive umask cannot strip them.
	if err := os.Chmod(path, fi.Mode().Perm()|0o700); err != nil {
		return "", fmt.Errorf("cannot stage runner executable: %w", err)
	}
	return path, nil
}

type boundedWriter struct {
	max      int64
	buf      *bytes.Buffer
	written  int64
	overflow bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.written += int64(len(p))
	space := w.max - int64(w.buf.Len())
	if space > 0 {
		if int64(len(p)) <= space {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:space])
		}
	}
	if w.written > w.max {
		w.overflow = true
	}
	return len(p), nil
}

func (w *boundedWriter) String() string { return w.buf.String() }
func (w *boundedWriter) Bytes() []byte  { return w.buf.Bytes() }

type runnerRequest struct {
	SchemaVersion   string         `json:"schema_version"`
	AnalyzerID      string         `json:"analyzer_id"`
	AnalyzerVersion string         `json:"analyzer_version"`
	Resource        publicResource `json:"resource"`
	Files           FileSet        `json:"files"`
	Config          AnalyzerConfig `json:"config"`
}

type publicResource struct {
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
	Samples []publicSample `json:"samples"`
}

type publicSample struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type runnerResponse struct {
	SchemaVersion   string    `json:"schema_version"`
	AnalyzerID      string    `json:"analyzer_id"`
	AnalyzerVersion string    `json:"analyzer_version"`
	Summary         string    `json:"summary"`
	Verdict         string    `json:"verdict"`
	Error           string    `json:"error"`
	Findings        []Finding `json:"findings"`
}

func (r *Runner) buildRequest(s Subject, cfg AnalyzerConfig) (runnerRequest, error) {
	var primary []publicSample
	for _, p := range s.samples {
		if p.isFile {
			continue
		}
		primary = append(primary, publicSample{Path: p.path, Content: p.content})
	}

	return runnerRequest{
		SchemaVersion:   "agent-marketplace.analyzer/v1",
		AnalyzerID:      r.id,
		AnalyzerVersion: r.ver,
		Resource: publicResource{
			Kind:    s.Resource.Kind().String(),
			Name:    s.Resource.Name(),
			Samples: primary,
		},
		Files:  s.Files,
		Config: cfg,
	}, nil
}

func (r *Runner) parseResponse(stdout []byte, envValues []string) (Audit, error) {
	if int64(len(stdout)) > r.cfg.MaxOutputBytes {
		return Audit{}, fmt.Errorf("runner response exceeded %d bytes", r.cfg.MaxOutputBytes)
	}

	// Redact every declared passthrough value from runner-controlled strings
	// before storing or echoing them in error text.
	redact := redactor(envValues)

	dec := json.NewDecoder(bytes.NewReader(stdout))
	dec.DisallowUnknownFields()
	var resp runnerResponse
	if err := dec.Decode(&resp); err != nil {
		return Audit{}, fmt.Errorf("runner response is not valid JSON: %s", redact(err.Error()))
	}

	// Reject trailing data after one JSON value.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Audit{}, fmt.Errorf("runner response contains trailing JSON")
		}
		if !strings.Contains(err.Error(), "EOF") {
			return Audit{}, fmt.Errorf("runner response has malformed trailing data: %s", redact(err.Error()))
		}
	}

	if resp.SchemaVersion != "agent-marketplace.analyzer/v1" {
		return Audit{}, fmt.Errorf("runner response schema version mismatch: %q", redact(resp.SchemaVersion))
	}
	if resp.AnalyzerID != r.id {
		return Audit{}, fmt.Errorf("runner response analyzer id mismatch: got %q, want %q", redact(resp.AnalyzerID), r.id)
	}
	if resp.AnalyzerVersion != r.ver {
		return Audit{}, fmt.Errorf("runner response analyzer version mismatch: got %q, want %q", redact(resp.AnalyzerVersion), r.ver)
	}
	if resp.Verdict == "" {
		return Audit{}, fmt.Errorf("runner response verdict is empty")
	}
	if !isValidVerdict(resp.Verdict) {
		return Audit{}, fmt.Errorf("runner response verdict is not valid: %q", redact(resp.Verdict))
	}

	resp.Summary = redact(resp.Summary)
	resp.Error = redact(resp.Error)
	prefix := r.id + "."
	for i := range resp.Findings {
		resp.Findings[i].Analyzer = r.id
		resp.Findings[i].AnalyzerVersion = r.ver
		resp.Findings[i].Code = redact(resp.Findings[i].Code)
		resp.Findings[i].Message = redact(resp.Findings[i].Message)
		if resp.Findings[i].SourceRange != nil {
			resp.Findings[i].SourceRange.File = redact(resp.Findings[i].SourceRange.File)
		}
		if resp.Findings[i].Code == "" {
			return Audit{}, fmt.Errorf("runner response finding %d has empty code", i)
		}
		if !strings.HasPrefix(resp.Findings[i].Code, prefix) {
			return Audit{}, fmt.Errorf("runner response finding %d has code %q, not namespaced to %q", i, resp.Findings[i].Code, r.id)
		}
	}

	if resp.Verdict == VerdictError {
		msg := resp.Error
		if msg == "" {
			msg = "external analyzer reported error"
		}
		return sanitizeAudit(Audit{
			AnalyzerID:      r.id,
			AnalyzerVersion: r.ver,
			Verdict:         VerdictError,
			Summary:         "audit unavailable",
			Error:           msg,
		}), nil
	}

	summary := resp.Summary
	if summary == "" {
		summary = summaryForAudit(resp.Verdict, resp.Findings)
	}

	slog.Debug("runner audit raw",
		"analyzer", r.id,
		"verdict", resp.Verdict,
		"findings", len(resp.Findings))

	return sanitizeAudit(Audit{
		AnalyzerID:      r.id,
		AnalyzerVersion: r.ver,
		Verdict:         resp.Verdict,
		Summary:         summary,
		Findings:        resp.Findings,
	}), nil
}

// redactor returns a function that replaces every declared passthrough value
// with a fixed redaction marker.
func redactor(secrets []string) func(string) string {
	vals := append([]string(nil), secrets...)
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	return func(s string) string {
		for _, v := range vals {
			if v == "" {
				continue
			}
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
		return s
	}
}

// redactStderr redacts declared passthrough values from captured stderr and
// sanitizes the result.
func redactStderr(stderr string, envValues []string) string {
	return SanitizeString(redactor(envValues)(stderr), MaxAuditErrorBytes)
}

// safeAnalyzerIdentifier checks that id is a dotted, safe identifier.
func safeAnalyzerIdentifier(id string) error {
	if id == "" {
		return errors.New("must not be empty")
	}
	valid := regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	if !valid.MatchString(id) {
		return fmt.Errorf("%q is not a valid analyzer identifier", id)
	}
	return nil
}

// validateEnvNames checks that the supplied environment variable names are
// syntactically valid and unique.
func validateEnvNames(names []string) error {
	seen := make(map[string]bool)
	for _, n := range names {
		if n == "" {
			return fmt.Errorf("empty environment variable name")
		}
		if !isValidEnvName(n) {
			return fmt.Errorf("invalid environment variable name %q", n)
		}
		if seen[n] {
			return fmt.Errorf("duplicate environment variable name %q", n)
		}
		seen[n] = true
	}
	return nil
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isValidEnvName(s string) bool {
	return envNameRe.MatchString(s)
}

// validateCommand checks that the runner command is a regular executable file.
func validateCommand(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("runner command must be a regular executable file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("runner command must be a regular executable file")
	}
	if fi.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("runner command must be executable")
	}
	return nil
}
