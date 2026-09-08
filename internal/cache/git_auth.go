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
	"syscall"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

var ErrGitAuthConfigUntrusted = errors.New("git.auth-config-untrusted: authentication configuration must use trusted system/global files without includes or repository-local authentication settings")

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
		"http.sslcainfo", "http.sslcapath", "http.sslcert", "http.sslkey", "http.sslcertpasswordprotected", "http.extraheader":
		return true
	}
	return false
}

func gitAuthEnvironment(ctx context.Context, repoDir string, env []string, source string) ([]string, error) {
	if repoDir != "" {
		cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "config", "--local", "--no-includes", "--null", "--list")
		cmd.Dir, cmd.Env = repoDir, env
		output, err := cmd.Output()
		if err != nil {
			return nil, ErrGitAuthConfigUntrusted
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
				return nil, ErrGitAuthConfigUntrusted
			}
			hasRewrite = hasRewrite || strings.HasPrefix(key, "url.") && strings.HasSuffix(key, ".insteadof")
		}
		if hasRewrite {
			cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "ls-remote", "--get-url", "--", source)
			cmd.Dir, cmd.Env = repoDir, env
			output, err := cmd.Output()
			if err != nil {
				return nil, ErrGitAuthConfigUntrusted
			}
			source = strings.TrimSpace(string(output))
		} else if remoteURL != "" {
			source = remoteURL
		}
	}
	canonical, err := lockfile.CanonicalGitURL(redactSecrets(source))
	if err != nil {
		parsed, err := url.Parse(source)
		if err != nil || parsed.Host == "" {
			return env, nil
		}
		canonical = strings.ToLower(parsed.Scheme) + "://"
	}
	if !strings.HasPrefix(canonical, "https://") && !strings.HasPrefix(canonical, "http://") {
		return env, nil
	}

	count := 0
	for _, scope := range []string{"system", "global"} {
		if scope == "system" {
			switch strings.ToLower(os.Getenv("GIT_CONFIG_NOSYSTEM")) {
			case "1", "true", "yes", "on":
				continue
			case "", "0", "false", "no", "off":
			default:
				return nil, ErrGitAuthConfigUntrusted
			}
		}
		configKey := "GIT_CONFIG_" + strings.ToUpper(scope)
		configuredPath, configured := os.LookupEnv(configKey)
		if configured && configuredPath == "" {
			return nil, ErrGitAuthConfigUntrusted
		}
		configPaths := []string{configuredPath}
		if scope == "global" && configPaths[0] == "" {
			home, err := os.UserHomeDir()
			if err != nil || !filepath.IsAbs(home) {
				return nil, ErrGitAuthConfigUntrusted
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
				return nil, ErrGitAuthConfigUntrusted
			}
			var readerEnv []string
			for _, entry := range env {
				key, _, _ := strings.Cut(entry, "=")
				if key != "GIT_CONFIG_GLOBAL" && key != "GIT_CONFIG_NOSYSTEM" {
					readerEnv = append(readerEnv, entry)
				}
			}
			if configPath != "" {
				readerEnv = append(readerEnv, configKey+"="+configPath)
			}
			scopeArgs := []string{"--system"}
			if scope == "global" {
				scopeArgs = []string{"--file", configPath}
			}
			args := append([]string{"config"}, scopeArgs...)
			args = append(args, "--no-includes", "--null", "--show-origin", "--get-regexp", `^(credential\.|http\.|include\.|includeif\.)`)
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Env = readerEnv
			output, err := cmd.Output()
			if err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) && exit.ExitCode() == 1 && len(output) == 0 {
					continue
				}
				return nil, ErrGitAuthConfigUntrusted
			}
			fields := strings.Split(string(output), "\x00")
			if len(fields)%2 != 1 || fields[len(fields)-1] != "" {
				return nil, ErrGitAuthConfigUntrusted
			}
			for i := 0; i+1 < len(fields); i += 2 {
				origin, record := fields[i], fields[i+1]
				key, value, hasValue := strings.Cut(record, "\n")
				lower := strings.ToLower(key)
				if strings.HasPrefix(lower, "include.") || strings.HasPrefix(lower, "includeif.") {
					return nil, ErrGitAuthConfigUntrusted
				}
				if !gitAuthKey(key) {
					continue
				}
				path, ok := strings.CutPrefix(origin, "file:")
				if !ok || !filepath.IsAbs(path) {
					return nil, ErrGitAuthConfigUntrusted
				}
				info, err := os.Stat(path)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
					return nil, ErrGitAuthConfigUntrusted
				}
				stat, ok := info.Sys().(*syscall.Stat_t)
				if !ok || stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) {
					return nil, ErrGitAuthConfigUntrusted
				}
				if !hasValue {
					if strings.HasSuffix(lower, ".usehttppath") || strings.HasSuffix(lower, ".sslcertpasswordprotected") {
						value = "true"
					} else {
						return nil, ErrGitAuthConfigUntrusted
					}
				}
				env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", count, key), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", count, value))
				count++
			}
		}
	}
	return append(env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", count)), nil
}
