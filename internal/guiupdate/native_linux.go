//go:build linux

package guiupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type linuxPlatform struct{}

func NewNative(format string) *Service {
	return New(Config{Format: format, Architecture: runtime.GOARCH, Linux: linuxPlatform{}})
}

func (linuxPlatform) DownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if dir := os.Getenv("XDG_DOWNLOAD_DIR"); filepath.IsAbs(dir) {
		return filepath.Clean(dir), nil
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(configHome) {
		configHome = filepath.Join(home, ".config")
	}
	if data, err := os.ReadFile(filepath.Join(configHome, "user-dirs.dirs")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || name != "XDG_DOWNLOAD_DIR" {
				continue
			}
			value = strings.Trim(strings.TrimSpace(value), "\"")
			switch {
			case value == "$HOME":
				return home, nil
			case strings.HasPrefix(value, "$HOME/"):
				return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(value, "$HOME/"))), nil
			case strings.HasPrefix(value, "${HOME}/"):
				return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(value, "${HOME}/"))), nil
			case strings.HasPrefix(value, "~/"):
				return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(value, "~/"))), nil
			case filepath.IsAbs(value):
				return filepath.Clean(value), nil
			}
		}
	}
	return filepath.Join(home, "Downloads"), nil
}

func (linuxPlatform) OpenPackage(ctx context.Context, packagePath string) error {
	return exec.CommandContext(ctx, "xdg-open", packagePath).Run()
}
