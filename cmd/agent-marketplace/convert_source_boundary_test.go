package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: registry-symlink-containment/direct-conversion-refuses-foreign-source-payloads
func TestConvertSourceRefusesForeignPayloadBeforePublication(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	source := t.TempDir()
	writeClaudePluginSkill(t, source, "source-probe")
	output := filepath.Join(t.TempDir(), "converted")
	if out, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--out", output, source); err != nil {
		t.Fatalf("valid native conversion control failed: %s %v", out, err)
	}
	installed := filepath.Join(output, "skills", "source-probe", "SKILL.md")
	before, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	bytesBefore, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign.md")
	payload := []byte("---\nname: source-probe\ndescription: foreign control\n---\n\nforeign inert body\n")
	if err := os.WriteFile(foreign, payload, 0o640); err != nil {
		t.Fatal(err)
	}
	foreignBefore, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(source, "skills", "source-probe", "SKILL.md")
	if err := os.Rename(primary, filepath.Join(source, "retained-primary.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, primary); err != nil {
		t.Fatal(err)
	}
	out, err := executeCommand("convert", "--autodetect", "--to", "factory", "--sync", "--out", output, source)
	if !errors.Is(err, fsboundary.ErrUnsafePath) {
		t.Fatalf("foreign conversion payload was accepted: %s %v", out, err)
	}
	if after, err := os.Stat(installed); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("conversion refusal changed installed generation: %v", err)
	}
	if data, err := os.ReadFile(installed); err != nil || string(data) != string(bytesBefore) {
		t.Fatalf("conversion refusal changed installed bytes: %q %v", data, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(foreignBefore, after) || foreignBefore.Mode() != after.Mode() || !foreignBefore.ModTime().Equal(after.ModTime()) {
		t.Fatalf("conversion refusal changed foreign metadata: %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != string(payload) {
		t.Fatalf("conversion refusal changed foreign bytes: %q %v", data, err)
	}
}

func TestConvertSourceContainedAliasAndHardLink(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	source := t.TempDir()
	writeClaudePluginSkill(t, source, "alias-probe")
	primary := filepath.Join(source, "skills", "alias-probe", "SKILL.md")
	authored := filepath.Join(source, "skills", "alias-probe", "authored.txt")
	if err := os.Rename(primary, authored); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("authored.txt", primary); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(authored)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "converted")
	if out, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--out", output, source); err != nil {
		t.Fatalf("contained conversion file alias failed: %s %v", out, err)
	}
	assertFileExists(t, filepath.Join(output, "skills", "alias-probe", "SKILL.md"))
	if err := os.Link(authored, filepath.Join(source, "retained.txt")); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "unpublished")
	out, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--out", fresh, source)
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("conversion multiply linked payload accepted: %s %v", out, err)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("unsafe source created output/staging root: %v", err)
	}
	if after, err := os.Stat(authored); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("conversion changed authored source metadata: %v", err)
	}
}
