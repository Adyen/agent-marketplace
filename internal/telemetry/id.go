package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ReadInstallationID returns the durable installation ID stored at path without
// creating one. It returns ("", nil) when the file does not exist.
func ReadInstallationID(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		slog.Error("failed to read installation id", "path", path, "error", err)
		return "", fmt.Errorf("read installation id: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", nil
	}
	return id, nil
}

// LoadInstallationID returns the durable installation ID stored at path.
//
// If no file exists, it creates the parent directories with 0o700 permissions,
// generates a random 32-byte (64 hex character) ID, writes it to path with 0o600
// permissions, and returns the ID.
func LoadInstallationID(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty installation id path")
	}

	slog.Debug("loading installation id", "path", path)

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			slog.Debug("loaded existing installation id", "path", path)
			return id, nil
		}
	} else if !os.IsNotExist(err) {
		slog.Error("failed to read installation id", "path", path, "error", err)
		return "", fmt.Errorf("read installation id: %w", err)
	}

	id, err := generateInstallationID()
	if err != nil {
		slog.Error("failed to generate installation id", "path", path, "error", err)
		return "", err
	}

	dir := StateRootFromInstallationIDPath(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Error("failed to create installation id directory", "path", path, "directory", dir, "error", err)
		return "", fmt.Errorf("create installation id directory: %w", err)
	}

	// Harden permissions even if the directory already existed with looser
	// permissions created by an earlier run.
	if err := os.Chmod(dir, 0o700); err != nil {
		slog.Warn("failed to chmod installation id directory", "path", path, "directory", dir, "error", err)
	}

	created, err := publishInstallationID(path, id)
	if err != nil {
		slog.Error("failed to write installation id", "path", path, "error", err)
		return "", err
	}
	if created {
		slog.Info("generated new installation id", "path", path)
		return id, nil
	}

	// Another caller won the race to create the file. Never replace its ID;
	// return the winner's value instead.
	winner, err := ReadInstallationID(path)
	if err != nil {
		return "", err
	}
	if winner == "" {
		return "", errors.New("installation id file exists but is empty")
	}
	slog.Debug("loaded installation id created by another caller", "path", path)
	return winner, nil
}

// publishInstallationID writes a complete ID to a temporary file in the target
// directory, then atomically publishes it without replacing an ID created by a
// concurrent caller. A hard link provides the no-replace publication primitive;
// both paths are in the same directory, so the final path is never visible
// until the temporary file is fully written and closed.
func publishInstallationID(path, id string) (bool, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, fmt.Errorf("create temporary installation id file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove temporary installation id file", "path", tmpPath, "error", err)
		}
	}()
	defer func() { _ = tmp.Close() }()

	if err := tmp.Chmod(0o600); err != nil {
		return false, fmt.Errorf("chmod temporary installation id file: %w", err)
	}
	if _, err := tmp.WriteString(id + "\n"); err != nil {
		return false, fmt.Errorf("write temporary installation id file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return false, fmt.Errorf("sync temporary installation id file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("close temporary installation id file: %w", err)
	}

	if err := os.Link(tmpPath, path); err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("publish installation id: %w", err)
	}
	return true, nil
}

// ResetInstallationID deletes the installation ID file at path and purges any
// queued payloads or diagnostic captures that carry the old ID.
func ResetInstallationID(path string) error {
	if path == "" {
		return errors.New("empty installation id path")
	}

	slog.Info("resetting installation id", "path", path)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Error("failed to remove installation id", "path", path, "error", err)
		return fmt.Errorf("remove installation id: %w", err)
	}

	// The installation ID lives under <state-root>/telemetry/installation-id.
	// Queued payloads and diagnostic captures are sibling directories under the
	// same telemetry root.
	queueRoot := queueRootFromInstallationIDPath(path)
	if err := os.RemoveAll(queueRoot); err != nil {
		slog.Error("failed to remove queued payloads", "path", queueRoot, "error", err)
		return fmt.Errorf("remove queued payloads: %w", err)
	}

	diagRoot := filepath.Join(StateRootFromInstallationIDPath(path), "diagnostic")
	_ = os.RemoveAll(diagRoot)

	slog.Info("reset installation id complete", "path", path)
	return nil
}

func generateInstallationID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate installation id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
