package session_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/session"
)

// spec: gui-local-api/configuration-stage-reports-target-harnesses
func TestConfigStageReportsTargetsBeforeAcquisition(t *testing.T) {
	home, _ := newUserFixture(t)
	skills := t.TempDir()
	writeUserFile(t, filepath.Join(skills, "remote", "SKILL.md"), "---\nname: remote\ndescription: Remote skill\n---\n# Remote\n")
	writeUserFile(t, filepath.Join(home, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  remote: {git: https://example.invalid/team.git, ref: main}\nresources:\n  remote: {all: true}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	released := make(chan struct{})
	prev := session.CloneOrPull
	session.CloneOrPull = func(string, string) (string, error) {
		select {
		case <-released:
			return skills, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	t.Cleanup(func() { session.CloneOrPull = prev })

	sawConfig, sawFinal := false, false
	for msg := range session.ReadUserSnapshotStages(ctx, home, session.CatalogLoadRefresh, 3) {
		if msg.Stage == session.SnapshotStageConfig && len(msg.TargetHarnesses) > 0 && !sawConfig {
			if len(msg.TargetHarnesses) != 1 || string(msg.TargetHarnesses[0]) != "devin" {
				t.Fatalf("config-stage targets = %v, want [devin]", msg.TargetHarnesses)
			}
			sawConfig = true
			close(released)
		}
		if msg.Stage == session.SnapshotStageFinal {
			sawFinal = true
		}
	}
	if !sawConfig {
		t.Fatal("config stage did not report target harnesses before acquisition completed")
	}
	if !sawFinal {
		t.Fatal("final stage was never delivered")
	}
}
