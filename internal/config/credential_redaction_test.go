package config

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlockedRegistryCredentialRedaction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	raw := "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.invalid/repo.git?token=YOUR_QUERY_HERE#YOUR_FRAGMENT_HERE"
	user := filepath.Join(dir, "user.yaml")
	writeFile(t, user, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_registries: []\nsources:\n  shared: {git: '"+raw+"', ref: main}\nresources: {shared: [skill/review]}\n")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "absent-org.yaml"), GlobalPath: user,
		RepoPath: filepath.Join(dir, "absent-repo.yaml"), LocalPath: filepath.Join(dir, "absent-local.yaml"),
	})
	err := errors.Join(ec.Errors...)
	var blocked *RegistryURLBlockedError
	if !errors.As(err, &blocked) || len(blocked.BlockedURLs) != 1 || blocked.BlockedURLs[0] != raw {
		t.Fatalf("blocked error identity lost: %v", err)
	}
	for _, text := range []string{err.Error(), logs.String()} {
		if !strings.Contains(text, "blocked by allowed_registries") || !strings.Contains(text, "example.invalid/repo.git") {
			t.Fatalf("missing redaction positive control: %s", text)
		}
		for _, secret := range []string{"YOUR_USER_HERE", "YOUR_TOKEN_HERE", "YOUR_QUERY_HERE", "YOUR_FRAGMENT_HERE"} {
			if strings.Contains(text, secret) {
				t.Errorf("blocked diagnostic exposed %s", secret)
			}
		}
	}
	patternError := &RegistryURLBlockedError{BlockedURLs: []string{raw}, AllowedPatterns: []string{raw}}
	if strings.Contains(patternError.Error(), "YOUR_") || patternError.AllowedPatterns[0] != raw {
		t.Error("pattern rendering leaked credentials or changed policy input")
	}
}
