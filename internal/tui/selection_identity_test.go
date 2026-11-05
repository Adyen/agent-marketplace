package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestMarketplaceSelectionIndexKeepsDistinctPins(t *testing.T) {
	d1 := "sha256:" + strings.Repeat("1", 64)
	d2 := "sha256:" + strings.Repeat("2", 64)
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{
		{RegistryID: "a", Kind: "mcpserver", Name: "probe", Digest: d2, Revision: "X:" + d1 + ":R"},
		{RegistryID: "a:" + d2 + ":X", Kind: "mcpserver", Name: "probe", Digest: d1, Revision: "R"},
	}}
	if errs := config.ValidateMarketplaceEntries([]config.MarketplaceEntry{entry}); len(errs) > 0 {
		t.Fatalf("pins rejected: %v", errs)
	}
	index := indexMarketplaceSelections(&config.Config{Marketplaces: []config.MarketplaceEntry{entry}})
	firstKey := marketplaceSelectionKey(marketplaceCatalogKey(entry, entry.Resources[0]), ir.KindMcpServer.String(), "probe")
	secondKey := marketplaceSelectionKey(marketplaceCatalogKey(entry, entry.Resources[1]), ir.KindMcpServer.String(), "probe")
	if firstKey == secondKey || index[firstKey][config.RepoCommitted].Pin.Digest != d2 || index[secondKey][config.RepoCommitted].Pin.Digest != d1 {
		t.Fatalf("selection index collapsed pins: %#v", index)
	}
}

func TestCompetingRegistryCleanupIsScopeBound(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  trusted: {git: https://trusted.example/repo.git, ref: main}\n  rival: {git: https://user-rival.example/repo.git, ref: main}\nresources:\n  trusted: {include: [hook/startup]}\n  rival: {include: [hook/startup, skill/review]}\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.RepoCommitted}, RegistryURL: "https://project.example/repo.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.Organization, Required: true}, RegistryURL: "https://required.example/repo.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.UserPersonal, IsMarketplace: true}, RegistryURL: "https://market.example/repo.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.SourceUnset}, RegistryURL: "https://unset.example/repo.git"},
	}
	act := toggleAction{scope: scopeGlobal, enable: true, kind: ir.KindSkill, skillName: "review", registryURL: "https://trusted.example/repo.git"}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cmd := excludeCompetingRegistriesCmd(repo, rows, act); cmd != nil {
		msg, ok := cmd().(configWrittenMsg)
		if !ok || msg.err != nil {
			t.Fatalf("cross-scope cleanup = %#v", msg)
		}
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(before) != string(after) {
		t.Fatalf("cross-scope cleanup changed user config: %q err=%v", after, err)
	}
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	userRows := []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.UserPersonal}, RegistryURL: "https://user-rival.example/repo.git", RegistryRef: "main"}}
	if cmd := excludeCompetingRegistriesCmd(repo, userRows, act); cmd == nil {
		t.Fatal("same-scope cleanup returned nil")
	} else if msg, ok := cmd().(configWrittenMsg); !ok || msg.err != nil {
		t.Fatalf("same-scope cleanup = %#v", msg)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(updated), "exclude: [skill/review]") {
		t.Fatalf("existing user rival exclude disappeared: %q err=%v", updated, err)
	}

	projectConfig := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(projectConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	projectInitial := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  rival: {git: https://project-rival.example/repo.git, ref: main}\nresources:\n  rival: {include: [skill/review]}\n"
	if err := os.WriteFile(projectConfig, []byte(projectInitial), 0o600); err != nil {
		t.Fatal(err)
	}
	projectAct := toggleAction{scope: scopeProject, enable: true, kind: ir.KindSkill, skillName: "review", registryURL: "https://trusted.example/repo.git"}
	projectRows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.RepoCommitted}, RegistryURL: "https://project-rival.example/repo.git", RegistryRef: "main"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.UserPersonal}, RegistryURL: "https://user-not-added.example/repo.git", RegistryRef: "main"},
	}
	if cmd := excludeCompetingRegistriesCmd(repo, projectRows, projectAct); cmd == nil {
		t.Fatal("project cleanup returned nil")
	} else if msg, ok := cmd().(configWrittenMsg); !ok || msg.err != nil {
		t.Fatalf("project cleanup = %#v", msg)
	}
	projectUpdated, err := os.ReadFile(projectConfig)
	if err != nil || !strings.Contains(string(projectUpdated), "exclude: [skill/review]") {
		t.Fatalf("project rival exclude missing: %q err=%v", projectUpdated, err)
	}
	if strings.Contains(string(projectUpdated), "user-not-added.example") {
		t.Fatalf("user rival promoted into project config: %q", projectUpdated)
	}
}

func TestCompetingRegistryCleanupV4(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := "apiVersion: agent-marketplace/v4\nsources:\n  trusted: {git: https://trusted.example/repo.git, ref: main}\n  rival: {git: https://user-rival.example/repo.git, ref: main}\nresources:\n  rival: {include: [skill/review]}\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.UserPersonal}, RegistryURL: "https://user-rival.example/repo.git", RegistryRef: "main"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.RepoCommitted}, RegistryURL: "https://project-rival.example/repo.git", RegistryRef: "main"},
	}
	act := toggleAction{scope: scopeGlobal, enable: true, kind: ir.KindSkill, skillName: "review", registryURL: "https://trusted.example/repo.git"}
	cmd := excludeCompetingRegistriesCmd(repo, rows, act)
	if cmd == nil {
		t.Fatal("v4 cleanup returned nil")
	}
	msg, ok := cmd().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("v4 cleanup = %#v", msg)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := config.ParseV4(updated)
	if err != nil {
		t.Fatalf("parse updated config: %v\n%s", err, updated)
	}
	if !slices.Contains(parsed.Groups["rival"].Exclude, "skill/review") {
		t.Fatalf("rival group exclusion missing:\n%s", updated)
	}
	if strings.Contains(string(updated), "registries:") || strings.Contains(string(updated), "skills:") {
		t.Fatalf("v4 cleanup wrote legacy keys:\n%s", updated)
	}
	if strings.Contains(string(updated), "project-rival.example") {
		t.Fatalf("cross-scope rival promoted into user config:\n%s", updated)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode changed: %v", info.Mode().Perm())
	}
}

func TestCompetingRegistryCleanupStaleBindingRefuses(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := "apiVersion: agent-marketplace/v4\nsources:\n  trusted: {git: https://trusted.example/repo.git, ref: main}\nresources:\n  trusted: {include: [skill/review]}\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.UserPersonal}, RegistryURL: "https://vanished.example/repo.git", RegistryRef: "main"},
	}
	act := toggleAction{scope: scopeGlobal, enable: true, kind: ir.KindSkill, skillName: "review", registryURL: "https://trusted.example/repo.git"}
	cmd := excludeCompetingRegistriesCmd(repo, rows, act)
	if cmd == nil {
		t.Fatal("stale cleanup returned nil")
	}
	msg, ok := cmd().(configWrittenMsg)
	if !ok || msg.err == nil {
		t.Fatalf("stale competitor binding did not error: %#v", msg)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(after) != initial {
		t.Fatalf("stale cleanup changed config: %q err=%v", after, err)
	}
}

func TestGlobalToggleV4RelativeDirectoryCleanupAndCreationBoundary(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const initial = "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: ./project-registry}\nresources:\n  local: {include: [skill/review]}\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	act := toggleAction{scope: scopeGlobal, kind: ir.KindSkill, skillName: "review", registryURL: filepath.Join(canonicalWorkspacePath(t, home), ".agents", "project-registry"), registryKind: config.V4SourceDirectory}
	msg, ok := runScopedToggle(repo, act)().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("disable existing relative user registry = %#v", msg)
	}
	updated, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(updated), "exclude: [skill/review]") {
		t.Fatalf("disabled selection missing: %q err=%v", updated, err)
	}
	act.enable = true
	act.registryURL = "./project-registry"
	msg, ok = runScopedToggle(repo, act)().(configWrittenMsg)
	var boundary *config.GlobalRelativeRegistryError
	if !ok || !errors.As(msg.err, &boundary) {
		t.Fatalf("enable existing relative user registry = %#v, want GlobalRelativeRegistryError", msg)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(updated) {
		t.Fatalf("rejected enable changed config: %q err=%v", after, err)
	}
	const empty = "apiVersion: agent-marketplace/v4\nsources: {}\n"
	if err := os.WriteFile(path, []byte(empty), 0o600); err != nil {
		t.Fatal(err)
	}
	act.enable = false
	msg, ok = runScopedToggle(repo, act)().(configWrittenMsg)
	if !ok || !errors.As(msg.err, &boundary) {
		t.Fatalf("disable absent relative user registry = %#v, want GlobalRelativeRegistryError", msg)
	}
	after, err = os.ReadFile(path)
	if err != nil || string(after) != empty {
		t.Fatalf("rejected creation changed config: %q err=%v", after, err)
	}
}

func TestValidateToggleRegistryWriteScopeBoundary(t *testing.T) {
	err := validateToggleRegistryWrite([]row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.RepoCommitted}, RegistryURL: "./project"}}, []error{errors.New(`skill "review" is provided by multiple registries`)}, toggleAction{scope: scopeGlobal, enable: true, kind: ir.KindSkill, skillName: "review", registryURL: "https://trusted.example/repo.git"})
	if err != nil {
		t.Fatalf("remote primary with cross-scope relative rival = %v", err)
	}
	err = validateToggleRegistryWrite(nil, nil, toggleAction{scope: scopeGlobal, enable: true, kind: ir.KindSkill, skillName: "review", registryURL: "./primary"})
	var boundary *config.GlobalRelativeRegistryError
	if !errors.As(err, &boundary) {
		t.Fatalf("relative primary error = %v, want GlobalRelativeRegistryError", err)
	}
}
