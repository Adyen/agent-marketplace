package cache

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

func credentialFreeGitURL(source string) (string, error) {
	lower := strings.ToLower(source)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return source, nil
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" {
		return "", errors.New("invalid HTTP Git URL")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), nil
}

func gitCredentialEnvironment(env []string, source string) ([]string, string, error) {
	public, err := credentialFreeGitURL(source)
	if err != nil {
		return nil, "", err
	}
	if public == source {
		return env, source, nil
	}
	count, index := 0, -1
	for i, entry := range env {
		if value, ok := strings.CutPrefix(entry, "GIT_CONFIG_COUNT="); ok {
			count, err = strconv.Atoi(value)
			if err != nil || count < 0 {
				return nil, "", ErrGitAuthConfigUntrusted
			}
			index = i
		}
	}
	env = slices.Clone(env)
	next := fmt.Sprintf("GIT_CONFIG_COUNT=%d", count+1)
	if index < 0 {
		env = append(env, next)
	} else {
		env[index] = next
	}
	env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=url.%s.insteadOf", count, source), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", count, public))
	return env, public, nil
}

func sanitizeLegacyGitOrigin(repo, source string) error {
	public, err := credentialFreeGitURL(source)
	if err != nil {
		return err
	}
	if public == source {
		return nil
	}
	root, err := os.OpenRoot(repo)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(".git")
	if err != nil || !info.IsDir() {
		return ErrGitAuthConfigUntrusted
	}
	info, err = root.Lstat(".git/config")
	if err != nil || !info.Mode().IsRegular() {
		return ErrGitAuthConfigUntrusted
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return ErrGitAuthConfigUntrusted
	}
	current, err := runGitOutput(repo, "config", "--local", "--no-includes", "--get-all", "remote.origin.url")
	if err != nil {
		return err
	}
	if current != public+"\n" {
		if err := runGit(repo, "config", "--local", "--no-includes", "--replace-all", "remote.origin.url", public); err != nil {
			return err
		}
	}
	if err := root.Remove(".git/FETCH_HEAD"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
