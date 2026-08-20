package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadInstallationID returns the durable installation ID stored at path.
//
// If no file exists, it creates the parent directories with 0o700 permissions,
// generates a random 32-byte (64 hex character) ID, writes it to path with 0o600
// permissions, and returns the ID.
func LoadInstallationID(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty installation id path")
	}

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read installation id: %w", err)
	}

	id, err := generateInstallationID()
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create installation id directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write installation id: %w", err)
	}

	return id, nil
}

// ResetInstallationID deletes the installation ID file at path and purges any
// queued payloads or diagnostic captures that carry the old ID.
func ResetInstallationID(path string) error {
	if path == "" {
		return errors.New("empty installation id path")
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove installation id: %w", err)
	}

	// The installation ID lives under <state-root>/telemetry/installation-id.
	// Queued payloads and diagnostic captures are sibling directories under the
	// same telemetry root.
	telemetryRoot := filepath.Dir(path)
	queueRoot := filepath.Join(telemetryRoot, "queue")
	if err := os.RemoveAll(queueRoot); err != nil {
		return fmt.Errorf("remove queued payloads: %w", err)
	}

	diagRoot := filepath.Join(telemetryRoot, "diagnostic")
	_ = os.RemoveAll(diagRoot)

	return nil
}

func generateInstallationID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate installation id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
