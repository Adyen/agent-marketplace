package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: doctor-repair-planning/destructive-lock-regeneration-preserves-backup-and-agreed-pins
func TestE2EDoctorRepairAcquisitionConflict(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	registry := env.CreateBareRepo("doctor-acquisition", RegistryFilesEx(
		[]Skill{{Name: "keeper", Description: "Pinned recovery resource"}}, nil, nil, nil,
	))
	env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  team: {git: '%s', ref: main}\nresources:\n  team: {include: [skill/keeper]}\n", registry))
	env.MustRunAM("lock", "--scope", "repo")
	env.MustRunAM("sync")
	lockPath := filepath.Join(env.workspace, ".agents", "lock.yaml")
	original, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	merged := []byte("<<<<<<< ours\n" + string(original) + "=======\n" + string(original) + ">>>>>>> theirs\n")
	if err := os.WriteFile(lockPath, merged, 0o644); err != nil {
		t.Fatal(err)
	}

	broken := env.RunAM("sync", "--check")
	if broken.ExitCode == 0 || !strings.Contains(broken.Combined, "lock.conflicted") {
		t.Fatalf("conflicted lock was not diagnosed (exit %d):\n%s", broken.ExitCode, broken.Combined)
	}
	repaired := runAMInTTY(t, env, "doctor", "--repair", "replace acquisition lock", "y\n")
	if repaired.ExitCode != 0 {
		t.Fatalf("confirmed acquisition repair failed (exit %d):\n%s", repaired.ExitCode, repaired.Combined)
	}
	matches, err := filepath.Glob(lockPath + ".repair-*.bak")
	if err != nil || len(matches) != 1 {
		t.Fatalf("repair backups = %v, %v; want one", matches, err)
	}
	backup, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Equal(backup, merged) {
		t.Fatalf("backup does not retain original conflict bytes: %v\nwant:\n%s\ngot:\n%s", err, merged, backup)
	}
	clean := env.RunAM("sync", "--check")
	if clean.ExitCode != 0 || strings.Contains(clean.Combined, "lock.") {
		t.Fatalf("sync --check after lock repair is not clean (exit %d):\n%s", clean.ExitCode, clean.Combined)
	}
}
