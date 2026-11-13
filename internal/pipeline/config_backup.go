package pipeline

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/config"
	"gopkg.in/yaml.v3"
)

const maxUserConfigFiles = 256

type UserConfigSnapshot struct {
	files []backup.ConfigFile
	err   error
}

func CaptureUserConfig() *UserConfigSnapshot {
	snapshot := &UserConfigSnapshot{}
	configPath, err := config.GlobalConfigPath()
	if err != nil {
		snapshot.err = err
		return snapshot
	}
	userDir, err := filepath.EvalSymlinks(filepath.Dir(configPath))
	if errors.Is(err, fs.ErrNotExist) {
		return snapshot
	}
	if err != nil {
		snapshot.err = fmt.Errorf("resolve user config directory: %w", err)
		return snapshot
	}
	lockPaths, err := config.ResolveConfigLockPathsTolerateMissing(configPath)
	if err != nil {
		snapshot.err = fmt.Errorf("resolve user config lock path: %w", err)
		return snapshot
	}

	visited := make(map[string]backup.ConfigFile)
	archived := make(map[string]bool)
	includeFiles := 0
	add := func(name string, file backup.ConfigFile) {
		if archived[name] {
			return
		}
		file.Name = name
		snapshot.files = append(snapshot.files, file)
		archived[name] = true
	}
	var visit func(string, string, bool) error
	visit = func(filePath, archiveName string, followIncludes bool) error {
		canonical, err := filepath.EvalSymlinks(filePath)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("resolve user config file %q: %w", filePath, err)
		}
		canonical, err = filepath.Abs(canonical)
		if err != nil {
			return fmt.Errorf("resolve user config file %q: %w", filePath, err)
		}
		if captured, ok := visited[canonical]; ok {
			if archiveName != "" {
				add(archiveName, captured)
			}
			return nil
		}
		if followIncludes && includeFiles >= maxUserConfigFiles {
			return fmt.Errorf("user config include tree exceeds %d files", maxUserConfigFiles)
		}
		data, info, err := readUserConfigFile(canonical)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read user config file %q: %w", canonical, err)
		}
		captured := backup.ConfigFile{Data: data, Mode: info.Mode(), ModTime: info.ModTime()}
		visited[canonical] = captured
		if followIncludes {
			includeFiles++
		}
		if archiveName != "" {
			add(archiveName, captured)
		} else if relative, ok := userConfigRelativePath(userDir, canonical); ok {
			add(relative, captured)
		} else {
			slog.Debug("user config include is outside the user config directory", "path", canonical)
		}

		if !followIncludes {
			return nil
		}
		var document struct {
			Include config.IncludePaths `yaml:"include"`
		}
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil
		}
		for _, include := range document.Include {
			includePath := include
			if !filepath.IsAbs(includePath) {
				includePath = filepath.Join(filepath.Dir(canonical), includePath)
			}
			if err := visit(includePath, "", true); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(configPath, "config.yaml", true); err != nil {
		snapshot.err = err
		return snapshot
	}
	if err := visit(lockPaths.RequestedLock, "lock.yaml", false); err != nil {
		snapshot.err = err
	}
	return snapshot
}

func (s *UserConfigSnapshot) WriteArchive() (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return backup.CreateConfigArchive(s.files)
}

func readUserConfigFile(path string) ([]byte, fs.FileInfo, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, nil, statErr
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, errors.New("not a regular file")
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, nil, err
	}
	return data, info, nil
}

func userConfigRelativePath(root, file string) (string, bool) {
	relative, err := filepath.Rel(root, file)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}
