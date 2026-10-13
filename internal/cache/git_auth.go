package cache

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

var ErrGitAuthConfigUntrusted = errors.New("git.auth-config-untrusted: authentication configuration must use trusted system/global files and ordinary include chains; conditional includes and repository-local authentication settings are unsupported")

func gitAcquisition(args []string) (string, bool) {
	for len(args) >= 2 && args[0] == "-c" {
		args = args[2:]
	}
	if len(args) == 0 || args[0] != "clone" && args[0] != "fetch" {
		return "", false
	}
	if i := slices.Index(args, "--"); i >= 0 && i+1 < len(args) {
		return args[i+1], true
	}
	if args[0] == "fetch" {
		if len(args) == 2 {
			return args[1], true
		}
		return "origin", true
	}
	return "", true
}

type gitTransportFailure struct {
	message string
	cause   error
}

func (e *gitTransportFailure) Error() string {
	return e.message
}

func (e *gitTransportFailure) Unwrap() error {
	return e.cause
}

func isGitTransportFailure(err error) bool {
	var failure *gitTransportFailure
	return errors.As(err, &failure)
}

// canonicalGitTransport canonicalizes source without its credentials. Redacted
// userinfo would turn an HTTP(S) URL with a port into an SCP-style address.
func canonicalGitTransport(source string) (string, error) {
	public, err := credentialFreeGitURL(source)
	if err != nil {
		return "", err
	}
	return lockfile.CanonicalGitURL(public)
}

func gitTransportFailureDiagnostic(source string) string {
	canonical, err := canonicalGitTransport(source)
	if err == nil && (strings.HasPrefix(canonical, "ssh://") || strings.HasPrefix(canonical, "git+scp://")) {
		return "git.auth-transport-failed: Git SSH connection failed. Verify your SSH agent and repository access"
	}
	return "git.auth-transport-failed: transport diagnostics withheld to protect authentication data"
}

func gitAuthKey(key string) bool {
	section, rest, ok := strings.Cut(key, ".")
	if !ok {
		return false
	}
	if i := strings.LastIndexByte(rest, '.'); i >= 0 {
		rest = rest[i+1:]
	}
	switch strings.ToLower(section) + "." + strings.ToLower(rest) {
	case "credential.helper", "credential.username", "credential.usehttppath",
		"http.sslcainfo", "http.sslcapath", "http.sslcert", "http.sslkey", "http.sslcertpasswordprotected", "http.extraheader",
		"http.followredirects":
		return true
	}
	return false
}

func gitAuthEnvironment(ctx context.Context, repoDir string, env []string, source string) ([]string, string, error) {
	if repoDir != "" {
		cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "config", "--local", "--no-includes", "--null", "--list")
		cmd.Dir, cmd.Env = repoDir, env
		output, err := cmd.Output()
		if err != nil {
			return nil, source, ErrGitAuthConfigUntrusted
		}
		remoteURL := ""
		hasRewrite := false
		for _, record := range strings.Split(string(output), "\x00") {
			key, value, _ := strings.Cut(record, "\n")
			if key == "remote."+source+".url" && remoteURL == "" {
				remoteURL = value
			}
			key = strings.ToLower(key)
			if strings.HasPrefix(key, "credential.") || strings.HasPrefix(key, "http.") || strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") || key == "core.askpass" || key == "extensions.worktreeconfig" {
				return nil, source, ErrGitAuthConfigUntrusted
			}
			hasRewrite = hasRewrite || strings.HasPrefix(key, "url.") && strings.HasSuffix(key, ".insteadof")
		}
		if hasRewrite {
			cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "ls-remote", "--get-url", "--", source)
			cmd.Dir, cmd.Env = repoDir, env
			output, err := cmd.Output()
			if err != nil {
				return nil, source, ErrGitAuthConfigUntrusted
			}
			source = strings.TrimSpace(string(output))
		} else if remoteURL != "" {
			source = remoteURL
		}
	}
	canonical, err := canonicalGitTransport(source)
	if err != nil {
		parsed, err := url.Parse(source)
		if err != nil || parsed.Host == "" {
			return env, source, nil
		}
		canonical = strings.ToLower(parsed.Scheme) + "://"
	}
	if !strings.HasPrefix(canonical, "https://") && !strings.HasPrefix(canonical, "http://") {
		return env, source, nil
	}

	reader := gitAuthConfigReader{ctx: ctx, env: env, active: make(map[[2]uint64]bool)}
	for _, scope := range []string{"system", "global"} {
		if scope == "system" {
			switch strings.ToLower(os.Getenv("GIT_CONFIG_NOSYSTEM")) {
			case "1", "true", "yes", "on":
				continue
			case "", "0", "false", "no", "off":
			default:
				return nil, source, ErrGitAuthConfigUntrusted
			}
		}
		configKey := "GIT_CONFIG_" + strings.ToUpper(scope)
		configuredPath, configured := os.LookupEnv(configKey)
		if configured && configuredPath == "" {
			return nil, source, ErrGitAuthConfigUntrusted
		}
		configPaths := []string{configuredPath}
		if scope == "global" && configPaths[0] == "" {
			home, err := os.UserHomeDir()
			if err != nil || !filepath.IsAbs(home) {
				return nil, source, ErrGitAuthConfigUntrusted
			}
			xdg := os.Getenv("XDG_CONFIG_HOME")
			if xdg == "" {
				xdg = filepath.Join(home, ".config")
			}
			configPaths = []string{filepath.Join(xdg, "git", "config"), filepath.Join(home, ".gitconfig")}
		}
		for _, configPath := range configPaths {
			if configPath == "/dev/null" {
				continue
			}
			if configPath != "" && !filepath.IsAbs(configPath) {
				return nil, source, ErrGitAuthConfigUntrusted
			}
			if configPath == "" {
				var err error
				configPath, err = gitSystemAuthConfigPath(ctx, env)
				if err != nil {
					return nil, source, err
				}
				if configPath == "" {
					continue
				}
			}
			if err := reader.read(configPath, 0); err != nil {
				return nil, source, err
			}
		}
	}
	for i, setting := range reader.settings {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, setting.key), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, setting.value))
	}
	return append(env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(reader.settings))), source, nil
}
