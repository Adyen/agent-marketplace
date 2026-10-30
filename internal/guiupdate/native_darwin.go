//go:build darwin

package guiupdate

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type darwinPlatform struct{}

func NewNative(format string) *Service {
	return New(Config{Format: format, Architecture: runtime.GOARCH, Mac: darwinPlatform{}})
}

func (darwinPlatform) AppPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	macOS := filepath.Dir(executable)
	contents := filepath.Dir(macOS)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return "", errors.New("running executable is not inside an app bundle")
	}
	return filepath.Dir(contents), nil
}

func (darwinPlatform) IsTranslocated(appPath string) (bool, error) {
	for current := appPath; current != filepath.Dir(current); current = filepath.Dir(current) {
		if filepath.Base(current) == "AppTranslocation" {
			return true, nil
		}
	}
	return false, nil
}

func (darwinPlatform) IsOnDiskImage(appPath string) (bool, error) {
	data, err := exec.Command("/usr/bin/hdiutil", "info", "-plist").Output()
	if err != nil {
		return false, fmt.Errorf("inspect mounted disk images: %w", err)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("parse disk image information: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "key" {
			continue
		}
		var key string
		if err := decoder.DecodeElement(&key, &start); err != nil {
			return false, fmt.Errorf("parse disk image information: %w", err)
		}
		if key != "mount-point" {
			continue
		}
		token, err = decoder.Token()
		if err != nil {
			return false, fmt.Errorf("parse disk image mount point: %w", err)
		}
		mount, ok := token.(xml.StartElement)
		if !ok || mount.Name.Local != "string" {
			continue
		}
		var mountPath string
		if err := decoder.DecodeElement(&mountPath, &mount); err != nil {
			return false, fmt.Errorf("parse disk image mount point: %w", err)
		}
		if pathWithin(mountPath, appPath) {
			return true, nil
		}
	}
}

func (darwinPlatform) ParentWritable(appPath string) (bool, error) {
	file, err := os.CreateTemp(filepath.Dir(appPath), ".agent-marketplace-write-check-")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return false, nil
		}
		return false, err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return false, err
	}
	if err := os.Remove(name); err != nil {
		return false, err
	}
	return true, nil
}

func (darwinPlatform) ReadBundleInfo(appPath string) (BundleInfo, error) {
	plist := filepath.Join(appPath, "Contents", "Info.plist")
	read := func(key string) (string, error) {
		output, err := exec.Command("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plist).Output()
		if err != nil {
			return "", fmt.Errorf("read %s from app Info.plist: %w", key, err)
		}
		return strings.TrimSpace(string(output)), nil
	}
	identifier, err := read("CFBundleIdentifier")
	if err != nil {
		return BundleInfo{}, err
	}
	version, err := read("CFBundleShortVersionString")
	if err != nil {
		return BundleInfo{}, err
	}
	buildVersion, err := read("CFBundleVersion")
	if err != nil {
		return BundleInfo{}, err
	}
	return BundleInfo{Identifier: identifier, Version: version, BuildVersion: buildVersion}, nil
}

func (darwinPlatform) VerifyCodeSignature(ctx context.Context, appPath string) error {
	return exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", "--deep", appPath).Run()
}

func (darwinPlatform) ExtractArchive(ctx context.Context, archivePath, destination string) (string, error) {
	if err := exec.CommandContext(ctx, "/usr/bin/ditto", "-x", "-k", archivePath, destination).Run(); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return "", err
	}
	var app string
	for _, entry := range entries {
		if entry.IsDir() && filepath.Ext(entry.Name()) == ".app" {
			if app != "" {
				return "", errors.New("update archive contains multiple app bundles")
			}
			app = filepath.Join(destination, entry.Name())
		}
	}
	if app == "" {
		return "", errors.New("update archive does not contain an app bundle")
	}
	return app, nil
}

func (darwinPlatform) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (darwinPlatform) RelaunchAfterExit(_ context.Context, appPath string) error {
	pid := strconv.Itoa(os.Getpid())
	script := `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done; exec /usr/bin/open -a "$2"`
	command := exec.Command("/bin/sh", "-c", script, "agent-marketplace-update-relaunch", pid, appPath)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func (darwinPlatform) RemoveAll(path string) error { return os.RemoveAll(path) }
