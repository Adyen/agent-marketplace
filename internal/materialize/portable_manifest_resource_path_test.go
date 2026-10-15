package materialize

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// portableNomination is an independently authored manifest entry that claims
// bytes file-mode materialization never recorded.
type portableNomination struct {
	target  hcc.Target
	machine bool
	// entry is the manifest path; disk is the nominated path below the scope
	// base (the repository or HOME).
	entry, disk string
	// tree nominates a directory that holds one handwritten resource.
	tree bool
	// rootID is the Codex partition identity the nomination claims.
	rootID string
}

func (n portableNomination) name() string {
	return fmt.Sprintf("%s/machine=%v/%s", n.target, n.machine, n.entry)
}

// spec: materialization/portable-manifest-cannot-nominate-root-files-or-component-directories
func TestRunPortableManifestCannotNominateRootFilesOrComponentDirectories(t *testing.T) {
	nominations := []portableNomination{
		{target: hcc.TargetClaude, entry: "settings.json", disk: ".claude/settings.json"},
		{target: hcc.TargetClaude, entry: "skills", disk: ".claude/skills", tree: true},
		{target: hcc.TargetFactory, entry: "mcp.json", disk: ".factory/mcp.json"},
		{target: hcc.TargetCursor, entry: "mcp.json", disk: ".cursor/mcp.json"},
		{target: hcc.TargetGemini, entry: "settings.json", disk: ".gemini/settings.json"},
		{target: hcc.TargetDevin, entry: "mcp_config.json", disk: ".devin/mcp_config.json"},
		{target: hcc.TargetWindsurf, entry: ".windsurf/hooks.json", disk: ".windsurf/hooks.json"},
		{target: hcc.TargetWindsurf, entry: ".windsurf/skills", disk: ".windsurf/skills", tree: true},
		{target: hcc.TargetClaude, machine: true, entry: "settings.json", disk: ".claude/settings.json"},
		{target: hcc.TargetFactory, machine: true, entry: "skills", disk: ".factory/skills", tree: true},
		{target: hcc.TargetCursor, machine: true, entry: "mcp.json", disk: ".cursor/mcp.json"},
		{target: hcc.TargetGemini, machine: true, entry: "skills", disk: ".gemini/skills", tree: true},
		{target: hcc.TargetDevin, machine: true, entry: "config.json", disk: ".config/devin/config.json"},
		{target: hcc.TargetWindsurf, machine: true, entry: "mcp_config.json", disk: ".codeium/windsurf/mcp_config.json"},
		{target: hcc.TargetFactory, entry: "hooks/hooks.json", disk: ".factory/hooks/hooks.json"},
		{target: hcc.TargetCursor, machine: true, entry: "hooks/hooks.json", disk: ".cursor/hooks/hooks.json"},
		{target: hcc.TargetClaude, entry: "hooks/hooks.json", disk: ".claude/hooks/hooks.json"},
		{target: hcc.TargetClaude, machine: true, entry: "agents", disk: ".claude/agents", tree: true},
		// Case-insensitive filesystems (the macOS and Windows defaults) resolve
		// a case variant to the protected location itself.
		{target: hcc.TargetFactory, entry: "HOOKS/Hooks.json", disk: ".factory/hooks/hooks.json"},
		{target: hcc.TargetClaude, machine: true, entry: "AGENTS", disk: ".claude/agents", tree: true},
	}
	for _, nomination := range nominations {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", nomination.name(), dryRun), func(t *testing.T) {
				repo, home := runNativePortableControl(t, nomination.target, nomination.machine)
				assertPortableNominationRefused(t, repo, home, nomination, dryRun)
			})
		}
	}
}

// spec: materialization/codex-manifest-cannot-nominate-shared-partition-content
func TestRunCodexManifestCannotNominateSharedPartitionContent(t *testing.T) {
	codex := hcc.Target("codex")
	nominations := []portableNomination{
		{target: codex, entry: ".agents/config.yaml", disk: ".agents/config.yaml", rootID: "repo-agents"},
		{target: codex, entry: ".agents/lock.yaml", disk: ".agents/lock.yaml", rootID: "repo-agents"},
		{target: codex, entry: ".agents/includes/team.yaml", disk: ".agents/includes/team.yaml", rootID: "repo-agents"},
		{target: codex, entry: ".agents/skills", disk: ".agents/skills", tree: true, rootID: "repo-agents"},
		{target: codex, entry: ".codex/config.toml", disk: ".codex/config.toml", rootID: "repo-codex"},
		{target: codex, entry: ".codex/rules/default.rules", disk: ".codex/rules/default.rules", rootID: "repo-codex"},
		{target: codex, machine: true, entry: ".agents/config.yaml", disk: ".agents/config.yaml", rootID: "machine-agents"},
		{target: codex, machine: true, entry: ".agents/lock.yaml", disk: ".agents/lock.yaml", rootID: "machine-agents"},
		{target: codex, machine: true, entry: ".codex/hooks.json", disk: ".codex/hooks.json", rootID: "machine-codex"},
	}
	for _, nomination := range nominations {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", nomination.name(), dryRun), func(t *testing.T) {
				repo, home := runNativePortableControl(t, nomination.target, nomination.machine)
				assertPortableNominationRefused(t, repo, home, nomination, dryRun)
			})
		}
	}
}

// runNativePortableControl installs and then genuinely prunes native
// file-mode output for target in one scope, proving the scope's portable
// lifecycle works before a nomination is attempted.
func runNativePortableControl(t *testing.T, target hcc.Target, machine bool) (string, string) {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	source, base := config.RepoCommitted, repo
	if machine {
		source, base = config.UserPersonal, home
	}
	skillRoots := map[hcc.Target][2]string{
		hcc.TargetClaude:   {".claude", ".claude"},
		hcc.TargetFactory:  {".factory", ".factory"},
		hcc.TargetCursor:   {".cursor", ".cursor"},
		hcc.TargetGemini:   {".gemini", ".gemini"},
		hcc.TargetDevin:    {".devin", ".config/devin"},
		hcc.TargetWindsurf: {".windsurf", ".codeium/windsurf"},
		"codex":            {".agents", ".agents"},
	}
	scope := 0
	if machine {
		scope = 1
	}
	resources := []ir.Resource{ir.NewSkill("native-probe", "native control", "inert body", nil, nil, ir.SourceRef{})}
	native := []string{filepath.Join(base, skillRoots[target][scope], "skills", "native-probe", "SKILL.md")}
	if target == "codex" {
		resources = append(resources, ir.NewSubagent("native-agent", "native control", "inert body", "", nil, "", nil, "", nil, ir.SourceRef{}))
		native = append(native, filepath.Join(base, ".codex", "agents", "native-agent.toml"))
	}
	var resolved []resolver.ResolvedArtifact
	for _, resource := range resources {
		resolved = append(resolved, resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: resource.Kind(), Name: resource.Name(), Resource: resource}, Source: source, Harnesses: []hcc.Target{target}})
	}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	installed, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(installed.Diags) {
		t.Fatalf("native %s install control failed: %+v %v", target, installed, err)
	}
	for _, path := range native {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("native %s control did not publish regular output %s: %v", target, path, err)
		}
		if data, err := os.ReadFile(path); err != nil || !bytes.Contains(data, []byte("inert body")) {
			t.Fatalf("native %s control published unexpected bytes at %s: %q %v", target, path, data, err)
		}
	}
	pruned, err := Run(t.Context(), repo, nil, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(pruned.Diags) || pruned.TotalRemoved() != len(resources) {
		t.Fatalf("native %s prune control failed: %+v %v", target, pruned, err)
	}
	for _, path := range native {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("native %s control did not prune %s: %v", target, path, err)
		}
	}
	return repo, home
}

func assertPortableNominationRefused(t *testing.T, repo, home string, nomination portableNomination, dryRun bool) {
	t.Helper()
	base, manifest := repo, filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	if nomination.machine {
		base, manifest = home, filepath.Join(home, ".agent-marketplace", "materialization-manifest.yaml")
	}
	nominated := filepath.Join(base, filepath.FromSlash(nomination.disk))
	body := nominatedBody(nomination.disk)
	payloads := writeNominatedPayload(t, nominated, nomination.tree, body)
	encoded := fmt.Sprintf("file\x00\x000640\x00%d\x00%s", len(body), body)
	if nomination.tree {
		encoded = fmt.Sprintf("dir\x00\x000755\x00dir\x00handwritten\x000755\x00file\x00handwritten/SKILL.md\x000640\x00%d\x00%s", len(body), body)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(encoded)))
	// The digest must describe the current bytes, so only nomination admission
	// can explain a refusal.
	if current, err := portablePathDigest(nominated); err != nil || current != digest {
		t.Fatalf("independently authored digest does not match nominated bytes: %s != %s (%v)", current, digest, err)
	}
	identity := ""
	if nomination.rootID != "" {
		identity = authoredCodexRootIdentity(t, repo, nomination)
	}
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte(fmt.Sprintf("version: 1\nentries:\n  - target: %s\n    path: %s\n%s    digest: %s\n", nomination.target, nomination.entry, identity, digest))
	if err := os.WriteFile(manifest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	watched := append(slices.Clone(payloads), nominated, manifest)
	before := make(map[string]os.FileInfo, len(watched))
	for _, path := range watched {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = info
	}
	listings := map[string][]string{filepath.Dir(nominated): nil, filepath.Dir(manifest): nil}
	for dir := range listings {
		listings[dir] = directoryNames(t, dir)
	}

	options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile})}
	if dryRun {
		options = append(options, WithDryRun())
	}
	report, err := Run(t.Context(), repo, nil, options...)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range payloads {
		if data, err := os.ReadFile(path); err != nil || string(data) != body {
			t.Fatalf("nomination removed or changed nominated bytes at %s: %q %v; report=%+v", path, data, err, report)
		}
	}
	for _, path := range watched {
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before[path], after) || before[path].Mode() != after.Mode() || !before[path].ModTime().Equal(after.ModTime()) {
			t.Fatalf("nomination refusal changed identity or metadata of %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(manifest); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("nomination refusal changed manifest evidence: %q %v", data, err)
	}
	for dir, names := range listings {
		if got := directoryNames(t, dir); !slices.Equal(got, names) {
			t.Fatalf("nomination refusal left residue in %s: %v, want %v", dir, got, names)
		}
	}
	if !hasDiagCode(report.Diags, "materialize.manifest.error") || report.TotalRemoved() != 0 {
		t.Fatalf("nomination was not refused without a removal receipt: %+v", report)
	}
}

// nominatedBody returns handwritten bytes valid for the nominated file's
// format, so native aggregate reconciliation reading the same file cannot
// explain the outcome.
func nominatedBody(path string) string {
	if strings.HasSuffix(path, ".toml") {
		return "handwritten = true\n"
	}
	return "{\"handwritten\": true}\n"
}

// writeNominatedPayload creates the handwritten bytes a nomination claims and
// returns the regular files that must survive refusal.
func writeNominatedPayload(t *testing.T, path string, tree bool, body string) []string {
	t.Helper()
	file := path
	if tree {
		file = filepath.Join(path, "handwritten", "SKILL.md")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if tree {
		for _, dir := range []string{path, filepath.Dir(file)} {
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(file, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}
	return []string{file}
}

// authoredCodexRootIdentity returns the manifest lines claiming the Codex
// partition root a writer would have recorded. Anyone who knows the absolute
// checkout path can author them.
func authoredCodexRootIdentity(t *testing.T, repo string, nomination portableNomination) string {
	t.Helper()
	partition, _, _ := strings.Cut(nomination.entry, "/")
	root := filepath.Join(repo, partition)
	if nomination.machine {
		root = filepath.Join(os.Getenv("HOME"), partition)
		if partition == ".codex" {
			root = os.Getenv("CODEX_HOME")
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	const domain = "agent-marketplace/ownership/root/v1"
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(domain+"\x00"+strconv.Itoa(len(canonical))+"\x00"+canonical)))
	// The authored identity must be the one installation records, so only
	// nomination admission can explain a refusal.
	destinations, err := harness.ResolveDestinations("codex", nomination.machine, repo)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, destination := range destinations {
		matched = matched || destination.ID == nomination.rootID && ownership.RootKey(destination.Root) == key
	}
	if !matched {
		t.Fatalf("authored Codex identity %s/%s matches no native destination: %+v", nomination.rootID, key, destinations)
	}
	return fmt.Sprintf("    root_id: %s\n    root_key: %s\n", nomination.rootID, key)
}

func directoryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
