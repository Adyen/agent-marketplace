package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type compatibilityCommand struct {
	Args            []string `json:"command"`
	ExitCode        int      `json:"exit_code"`
	Error           string   `json:"error,omitempty"`
	TimedOut        bool     `json:"timed_out,omitempty"`
	Observed        []string `json:"asserted_observations,omitempty"`
	Diagnostics     []string `json:"safe_diagnostics,omitempty"`
	OutputBytes     int      `json:"output_bytes"`
	OutputTruncated bool     `json:"output_truncated,omitempty"`
	output          string
}

const compatibilityOutputLimit = 256 << 10

type compatibilityOutput struct {
	data  []byte
	total int
}

func (o *compatibilityOutput) Write(data []byte) (int, error) {
	o.total += len(data)
	o.data = append(o.data, data[:min(len(data), compatibilityOutputLimit-len(o.data))]...)
	return len(data), nil
}

func compatibilityDiagnostics(output string) []string {
	var found []string
	for _, diagnostic := range []struct{ pattern, code string }{
		{`(?i)unknown command|unrecognized command|invalid command`, "cli.unknown-command"},
		{`(?i)unknown (option|flag)|unrecognized (option|flag)|unexpected argument`, "cli.unknown-option"},
		{`(?i)log[ -]?in required|not (logged|signed) in|authentication required|please (log|sign) in|requires authentication`, "cli.authentication-required"},
		{`(?i)not trusted|untrusted (project|workspace|directory)|trust (required|this)|pending.*trust`, "cli.project-trust-required"},
		{`(?i)interactive (terminal|session)|requires.*tty|not a tty`, "cli.interactive-only"},
		{`(?i)(parse|invalid|malformed).*(config|json|toml)|configuration.*error`, "cli.config-parse-error"},
		{`(?i)failed to connect|connection failed|disconnected`, "cli.mcp-connection-failed"},
		{`(?i)no (configured |mcp )?servers|no mcp servers configured`, "cli.mcp-no-servers"},
		{`(?i)no skills (found|installed|available)`, "cli.skills-no-resources"},
		{`(?i)permission denied|operation not permitted`, "cli.permission-denied"},
		{`(?i)auto.updat(e|ing)|updating.*(cli|binary)`, "cli.auto-update-message"},
	} {
		if regexp.MustCompile(diagnostic.pattern).MatchString(output) {
			found = append(found, diagnostic.code)
		}
	}
	return found
}

func compatibilityEnv(e *Env) []string {
	return []string{
		"HOME=" + e.home,
		"PATH=" + e.path,
		"XDG_CONFIG_HOME=" + filepath.Join(e.home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(e.home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(e.home, ".local", "state"),
		"XDG_DATA_HOME=" + filepath.Join(e.home, ".local", "share"),
		"XDG_CONFIG_DIRS=" + filepath.Join(e.home, ".config-dirs"),
		"XDG_DATA_DIRS=" + filepath.Join(e.home, ".data-dirs"),
		"XDG_RUNTIME_DIR=" + filepath.Join(e.home, ".runtime"),
		"CODEX_HOME=" + filepath.Join(e.home, ".codex"),
		"TMPDIR=" + filepath.Join(e.home, ".tmp"),
		"TMP=" + filepath.Join(e.home, ".tmp"),
		"TEMP=" + filepath.Join(e.home, ".tmp"),
		"LANG=C", "LC_ALL=C", "TERM=dumb", "NO_COLOR=1", "CLICOLOR=0", "CI=1",
		"DISABLE_AUTOUPDATER=1", "FACTORY_DROID_AUTO_UPDATE_ENABLED=false",
		"SHELL=/bin/sh", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TEMPLATE_DIR=" + filepath.Join(e.home, ".git-template"),
		"AGENT_MARKETPLACE_V3_ORG_CONFIG=" + filepath.Join(e.etc, "config.yaml"),
	}
}

func runCompatibilityCommand(e *Env, binary string, args []string, timeout time.Duration) compatibilityCommand {
	r := compatibilityCommand{Args: append([]string{binary}, args...), ExitCode: -1}
	if !filepath.IsAbs(binary) {
		r.Error = "executable must be configured with an absolute path"
		return r
	}
	for _, dir := range []string{".tmp", ".runtime", ".git-template", ".config-dirs", ".data-dirs"} {
		if err := os.MkdirAll(filepath.Join(e.home, dir), 0o700); err != nil {
			r.Error = "cannot create isolated runtime directory"
			return r
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = e.workspace
	cmd.Env = compatibilityEnv(e)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var output compatibilityOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	r.output = string(output.data)
	r.OutputBytes = output.total
	r.OutputTruncated = output.total > compatibilityOutputLimit
	r.Diagnostics = compatibilityDiagnostics(r.output)
	if cmd.ProcessState != nil {
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		r.TimedOut = ctx.Err() != nil
		switch {
		case r.TimedOut:
			r.Error = "inspection timed out"
		case cmd.ProcessState == nil:
			r.Error = "configured executable could not be started"
		default:
			r.Error = fmt.Sprintf("inspection exited %d; authentication and interactive recovery are forbidden", r.ExitCode)
		}
	}
	if r.OutputTruncated {
		r.Error = "inspection output exceeded bounded capture limit"
	}
	if r.Error != "" && len(r.Diagnostics) > 0 {
		r.Error += "; " + strings.Join(r.Diagnostics, ", ")
	}
	return r
}

func requireCompatibilityObservation(r *compatibilityCommand, absent bool, values ...string) error {
	if r.Error != "" || r.ExitCode != 0 {
		return fmt.Errorf("%s", r.Error)
	}
	for _, code := range r.Diagnostics {
		switch code {
		case "cli.authentication-required", "cli.unknown-command", "cli.unknown-option", "cli.interactive-only", "cli.config-parse-error", "cli.permission-denied":
			return fmt.Errorf("native inspection reported %s despite a successful exit", code)
		}
	}
	for _, value := range values {
		present := strings.Contains(r.output, value)
		if value == "" || present == absent {
			return fmt.Errorf("expected %q present=%t", value, !absent)
		}
	}
	for _, value := range values {
		if absent {
			value = "absent: " + value
		}
		r.Observed = append(r.Observed, value)
	}
	return nil
}
