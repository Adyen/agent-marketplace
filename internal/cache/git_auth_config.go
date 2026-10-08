package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	maxGitAuthFileBytes  = 4 << 20
	maxGitAuthTotalBytes = 16 << 20
	maxGitAuthDepth      = 10
	maxGitAuthFileVisits = 128
)

type gitAuthSetting struct {
	key, value string
}

type gitAuthConfigReader struct {
	ctx      context.Context
	env      []string
	active   map[[2]uint64]bool
	bytes    int
	visits   int
	settings []gitAuthSetting
}

// gitSystemAuthConfigPath discovers Git's compiled-in system path without
// following includes. Explicit GIT_CONFIG_SYSTEM paths do not need discovery.
// Keeping this native interface also supports Git versions without git var's
// newer GIT_CONFIG_SYSTEM variable.
func gitSystemAuthConfigPath(ctx context.Context, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "config", "--system", "--no-includes", "--null", "--show-origin", "--name-only", "--get-regexp", `^(credential\.|http\.|include\.|includeif\.)`)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GIT_CONFIG_NOSYSTEM=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_DIR=/dev/null")
	output := &gitAuthOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 && output.Len() == 0 {
			return "", nil
		}
		return "", ErrGitAuthConfigUntrusted
	}
	fields := strings.Split(output.String(), "\x00")
	if len(fields)%2 != 1 || fields[len(fields)-1] != "" {
		return "", ErrGitAuthConfigUntrusted
	}
	if len(fields) == 1 {
		return "", nil
	}
	path, ok := strings.CutPrefix(fields[0], "file:")
	if !ok || !filepath.IsAbs(path) {
		return "", ErrGitAuthConfigUntrusted
	}
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] != fields[0] {
			return "", ErrGitAuthConfigUntrusted
		}
	}
	return path, nil
}

// gitPrefixIncludePath lets Git expand its relocatable runtime prefix, which
// only the installed Git knows. The value travels through the environment so
// it needs no config quoting and is never itself treated as an include.
func gitPrefixIncludePath(ctx context.Context, env []string, value string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "config", "--null", "--type=path", "--get", "agent-marketplace.include")
	cmd.Env = append(append([]string(nil), env...), "GIT_DIR=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=agent-marketplace.include", "GIT_CONFIG_VALUE_0="+value)
	output := &gitAuthOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return "", ErrGitAuthConfigUntrusted
	}
	path, ok := strings.CutSuffix(output.String(), "\x00")
	if !ok || path == "" || strings.Contains(path, "\x00") {
		return "", ErrGitAuthConfigUntrusted
	}
	return path, nil
}

// gitAuthOutput bounds native parser output as well as file input. Never
// retain stderr: Git's parser diagnostics may include configuration values.
type gitAuthOutput struct {
	buffer bytes.Buffer
}

func (b *gitAuthOutput) Len() int       { return b.buffer.Len() }
func (b *gitAuthOutput) String() string { return b.buffer.String() }

func (b *gitAuthOutput) Write(data []byte) (int, error) {
	if len(data) > maxGitAuthTotalBytes-b.Len() {
		return 0, ErrGitAuthConfigUntrusted
	}
	return b.buffer.Write(data)
}

func (r *gitAuthConfigReader) read(path string, depth int) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.visits++
	if !filepath.IsAbs(path) || depth > maxGitAuthDepth || r.visits > maxGitAuthFileVisits {
		return ErrGitAuthConfigUntrusted
	}
	if path == os.DevNull {
		return nil
	}
	// Config symlinks remain supported. Validate the opened target rather than
	// trusting a path Stat followed by Git reopening the possibly replaced path.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ErrGitAuthConfigUntrusted
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Size() > maxGitAuthFileBytes {
		return ErrGitAuthConfigUntrusted
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) {
		return ErrGitAuthConfigUntrusted
	}
	id := [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}
	if r.active[id] {
		return ErrGitAuthConfigUntrusted
	}
	r.active[id] = true
	defer delete(r.active, id)
	if err := syscall.SetNonblock(int(file.Fd()), false); err != nil {
		return ErrGitAuthConfigUntrusted
	}
	limit := min(maxGitAuthFileBytes, maxGitAuthTotalBytes-r.bytes)
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) > limit {
		return ErrGitAuthConfigUntrusted
	}
	r.bytes += len(data)
	after, err := file.Stat()
	if err != nil || after.Size() != info.Size() || after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
		return ErrGitAuthConfigUntrusted
	}
	cmd := exec.CommandContext(r.ctx, "git", "config", "--file", "-", "--no-includes", "--null", "--list")
	cmd.Env = append(append([]string(nil), r.env...), "GIT_DIR=/dev/null")
	cmd.Stdin = bytes.NewReader(data)
	output := &gitAuthOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return ErrGitAuthConfigUntrusted
	}
	records := strings.Split(output.String(), "\x00")
	if records[len(records)-1] != "" {
		return ErrGitAuthConfigUntrusted
	}
	for _, record := range records[:len(records)-1] {
		key, value, hasValue := strings.Cut(record, "\n")
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "includeif.") {
			return ErrGitAuthConfigUntrusted
		}
		if lower == "include.path" {
			if !hasValue || value == "" {
				return ErrGitAuthConfigUntrusted
			}
			include := value
			switch {
			case strings.HasPrefix(include, "%(prefix)/"):
				var err error
				if include, err = gitPrefixIncludePath(r.ctx, r.env, include); err != nil {
					return err
				}
			case strings.HasPrefix(include, "~/"):
				home, err := os.UserHomeDir()
				if err != nil || !filepath.IsAbs(home) {
					return ErrGitAuthConfigUntrusted
				}
				include = home + include[1:]
			case strings.HasPrefix(include, "~"):
				return ErrGitAuthConfigUntrusted
			}
			// Join without lexical cleaning, as Git does, so ".." after a
			// symlinked directory resolves through its physical parent.
			if !filepath.IsAbs(include) {
				include = path[:strings.LastIndexByte(path, '/')+1] + include
			}
			if err := r.read(include, depth+1); err != nil {
				return err
			}
			continue
		}
		if !gitAuthKey(key) {
			continue
		}
		if !hasValue {
			if strings.HasSuffix(lower, ".usehttppath") || strings.HasSuffix(lower, ".sslcertpasswordprotected") {
				value = "true"
			} else {
				return ErrGitAuthConfigUntrusted
			}
		}
		r.settings = append(r.settings, gitAuthSetting{key: key, value: value})
	}
	slog.Debug("git authentication config read", "depth", depth, "bytes", len(data), "expanded_files", r.visits)
	return nil
}
