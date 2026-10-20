package materialize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/packagegen"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: plugin-delivery/unreferenced-generations-are-collected
func TestPluginGenerationGCWaitsUntilInstanceCommitReferencesIt(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	stateRoot := filepath.Join(root, "state")
	for _, dir := range []string{home, repo, stateRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", stateRoot)

	packageRoot := filepath.Join(root, "plugin")
	files := []ir.PluginFile{
		writePluginPackageFile(t, packageRoot, ".claude-plugin/plugin.json", `{"name":"pause-kit"}`, 0o644),
		writePluginPackageFile(t, packageRoot, "skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\n", 0o644),
		writePluginPackageFile(t, packageRoot, "hooks/hooks.json", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}`, 0o644),
		writePluginPackageFile(t, packageRoot, "scripts/check.sh", "#!/bin/sh\necho check\n", 0o755),
	}
	skill := ir.NewSkill("review", "Review", "Review body\n", nil, nil, ir.SourceRef{File: "skills/review/SKILL.md"})
	hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	plugin := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "pause-kit"},
		Members:  []ir.PluginMember{{Resource: skill}, {Resource: hook}},
		Files:    files,
	}
	pluginPackage := func(memberPath string) *resolver.PluginPackage {
		return &resolver.PluginPackage{
			Name:               "pause-kit",
			PackageRoot:        packageRoot,
			CatalogFingerprint: "test-catalog",
			Delivery:           "decompose",
			Scope:              config.RepoCommitted.String(),
			MemberPath:         memberPath,
			Package:            plugin,
		}
	}
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:         catalog.Entry{Kind: ir.KindSkill, Name: skill.Name(), Path: filepath.Join(packageRoot, "skills", "review", "SKILL.md"), Resource: skill},
			RegistryURL:   packageRoot,
			V4Kind:        config.V4SourceDirectory,
			Source:        config.RepoCommitted,
			Harnesses:     []hcc.Target{hcc.TargetClaude},
			InstalledName: "pause-kit-review",
			PluginPackage: pluginPackage("skills/review/SKILL.md"),
		},
		{
			Entry:         catalog.Entry{Kind: ir.KindHook, Name: hook.Name(), Path: filepath.Join(packageRoot, "hooks", "hooks.json"), Resource: hook},
			RegistryURL:   packageRoot,
			V4Kind:        config.V4SourceDirectory,
			Source:        config.RepoCommitted,
			Harnesses:     []hcc.Target{hcc.TargetClaude},
			InstalledName: "pause-kit-guard",
			PluginPackage: pluginPackage("hooks/hooks.json"),
		},
	}

	paused := make(chan struct{})
	resume := make(chan struct{})
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	runDone := make(chan error, 1)
	fault := func(point dependencytxn.FaultPoint) error {
		if point.Stage == dependencytxn.FaultAfterStage && filepath.Base(filepath.Dir(point.Path)) == "plugin-packages" {
			select {
			case <-paused:
			default:
				close(paused)
				<-resume
			}
		}
		return nil
	}
	go func() {
		_, err := Run(context.Background(), repo, resolved,
			WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}),
			WithPluginStateReconciliation(),
			withNativeHookSupportCommitFault(fault),
		)
		runDone <- err
	}()
	select {
	case <-paused:
	case err := <-runDone:
		t.Fatalf("materialization returned before the instance commit pause: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("plugin instance commit did not reach its dependency transaction fault hook")
	}
	joined := false
	defer func() {
		release()
		if !joined {
			select {
			case <-runDone:
			case <-time.After(10 * time.Second):
				t.Error("materialization did not finish after releasing the commit pause")
			}
		}
	}()

	generations, err := filepath.Glob(filepath.Join(stateRoot, "agent-marketplace", "packages", "[0-9a-f]*"))
	if err != nil || len(generations) != 1 {
		t.Fatalf("published generations while instance commit is paused = %v / %v, want one", generations, err)
	}
	generation := generations[0]
	gcDone := make(chan error, 1)
	gcStarted := make(chan struct{})
	go func() {
		close(gcStarted)
		_, err := packagegen.GarbageCollect(false)
		gcDone <- err
	}()
	<-gcStarted
	select {
	case err := <-gcDone:
		t.Fatalf("gc completed before the instance commit while its generation was unreferenced: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(generation, "scripts", "check.sh")); err != nil {
		t.Fatalf("gc removed the published package before its instance commit: %v", err)
	}
	release()
	if err := <-runDone; err != nil {
		t.Fatalf("materialize after commit pause: %v", err)
	}
	joined = true
	if err := <-gcDone; err != nil {
		t.Fatalf("gc after instance commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(generation, "scripts", "check.sh")); err != nil {
		t.Fatalf("committed plugin generation was collected: %v", err)
	}
	skillOutput, err := os.ReadFile(filepath.Join(repo, ".claude", "skills", "pause-kit-review", "SKILL.md"))
	if err != nil || !strings.Contains(string(skillOutput), "Review body") {
		t.Fatalf("plugin member output did not converge: %v\n%s", err, skillOutput)
	}
	settings, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(settings), filepath.Join(generation, "scripts", "check.sh")) {
		t.Fatalf("plugin root did not resolve inside the retained generation: %v\n%s", err, settings)
	}
	stateFiles, err := filepath.Glob(filepath.Join(stateRoot, "agent-marketplace", "ownership", "plugin-packages", "*.json"))
	if err != nil || len(stateFiles) != 1 {
		t.Fatalf("plugin instance state = %v / %v, want one record", stateFiles, err)
	}
	var state struct {
		Instance struct {
			GenerationKey string `json:"generation_key"`
		} `json:"instance"`
	}
	stateBytes, err := os.ReadFile(stateFiles[0])
	if err != nil || json.Unmarshal(stateBytes, &state) != nil || state.Instance.GenerationKey != filepath.Base(generation) {
		t.Fatalf("instance state does not reference committed generation: %v\n%s", err, stateBytes)
	}
}

func TestPluginInstanceCommitFailureDoesNotBlockLaterInstance(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	stateRoot := filepath.Join(root, "state")
	for _, dir := range []string{home, repo, stateRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", stateRoot)

	resolvedPlugin := func(name, body string) resolver.ResolvedArtifact {
		t.Helper()
		packageRoot := filepath.Join(repo, "vendor", name)
		memberPath := "skills/guide/SKILL.md"
		source := "---\nname: guide\ndescription: Guide\n---\n" + body
		files := []ir.PluginFile{writePluginPackageFile(t, packageRoot, memberPath, source, 0o644)}
		skill := ir.NewSkill("guide", "Guide", body, nil, nil, ir.SourceRef{File: memberPath})
		plugin := &ir.Plugin{
			Format:   "claude-plugin",
			Manifest: ir.Manifest{Name: name},
			Members:  []ir.PluginMember{{Resource: skill}},
			Files:    files,
		}
		return resolver.ResolvedArtifact{
			Entry:         catalog.Entry{Kind: ir.KindSkill, Name: name + "-guide", Path: filepath.Join(packageRoot, filepath.FromSlash(memberPath)), Resource: skill},
			RegistryURL:   packageRoot,
			V4Kind:        config.V4SourceDirectory,
			Source:        config.RepoCommitted,
			Harnesses:     []hcc.Target{hcc.TargetClaude},
			InstalledName: name + "-guide",
			PluginPackage: &resolver.PluginPackage{
				Name:               name,
				PackageRoot:        packageRoot,
				CatalogFingerprint: "test-catalog",
				Delivery:           "decompose",
				Scope:              config.RepoCommitted.String(),
				MemberPath:         memberPath,
				Package:            plugin,
			},
		}
	}
	initial := []resolver.ResolvedArtifact{
		resolvedPlugin("alpha-kit", "alpha original\n"),
		resolvedPlugin("beta-kit", "beta original\n"),
	}
	modes := WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile})
	if report, err := Run(context.Background(), repo, initial, modes, WithPluginStateReconciliation()); err != nil {
		t.Fatalf("initial plugin sync: %v (%+v)", err, report)
	}
	readMember := func(name string) string {
		t.Helper()
		path := filepath.Join(repo, ".claude", "skills", name+"-guide", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(data)
	}
	priorRefs, err := ownership.ReadPluginInstanceStates()
	if err != nil {
		t.Fatal(err)
	}
	priorGeneration := make(map[string]string, len(priorRefs))
	for _, ref := range priorRefs {
		priorGeneration[ref.Name] = ref.GenerationKey
	}
	if priorGeneration["alpha-kit"] == "" || priorGeneration["beta-kit"] == "" {
		t.Fatalf("initial plugin instance states = %+v", priorRefs)
	}

	updated := []resolver.ResolvedArtifact{
		resolvedPlugin("alpha-kit", "alpha updated\n"),
		resolvedPlugin("beta-kit", "beta updated\n"),
	}
	injected := errors.New("injected alpha instance commit fault")
	faulted := false
	fault := func(point dependencytxn.FaultPoint) error {
		if !faulted && point.Stage == dependencytxn.FaultAfterStage &&
			strings.Contains(filepath.ToSlash(point.Path), "/alpha-kit-guide/") {
			faulted = true
			return injected
		}
		return nil
	}
	report, err := Run(context.Background(), repo, updated, modes, WithPluginStateReconciliation(), withNativeHookSupportCommitFault(fault))
	if err != nil {
		t.Fatalf("sync after injected instance fault: %v", err)
	}
	if !faulted {
		t.Fatal("fault hook did not reach the alpha plugin member participant")
	}
	if got := readMember("alpha-kit"); !strings.Contains(got, "alpha original") || strings.Contains(got, "alpha updated") {
		t.Fatalf("failed alpha projection changed: %s", got)
	}
	if got := readMember("beta-kit"); !strings.Contains(got, "beta updated") {
		t.Fatalf("beta instance was not delivered after alpha failed: %s; diagnostics=%+v", got, report.Diags)
	}
	var alphaDiagnostic bool
	for _, diagnostic := range report.Diags {
		if diagnostic.Resource == "plugin/alpha-kit" && strings.Contains(diagnostic.Message, injected.Error()) {
			alphaDiagnostic = true
			break
		}
	}
	if !alphaDiagnostic {
		t.Fatalf("alpha commit failure diagnostic does not name its instance: %+v", report.Diags)
	}
	currentRefs, err := ownership.ReadPluginInstanceStates()
	if err != nil {
		t.Fatal(err)
	}
	currentGeneration := make(map[string]string, len(currentRefs))
	for _, ref := range currentRefs {
		currentGeneration[ref.Name] = ref.GenerationKey
	}
	if currentGeneration["alpha-kit"] != priorGeneration["alpha-kit"] {
		t.Fatalf("failed alpha state changed from %s to %s", priorGeneration["alpha-kit"], currentGeneration["alpha-kit"])
	}
	if currentGeneration["beta-kit"] == priorGeneration["beta-kit"] || currentGeneration["beta-kit"] == "" {
		t.Fatalf("beta state was not committed after alpha failed: before=%s after=%s", priorGeneration["beta-kit"], currentGeneration["beta-kit"])
	}
}

func writePluginPackageFile(t *testing.T, root, rel, body string, mode os.FileMode) ir.PluginFile {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(body)
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return ir.PluginFile{Path: rel, Digest: hex.EncodeToString(digest[:]), Executable: mode&0o111 != 0}
}
