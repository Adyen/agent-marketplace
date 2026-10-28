package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const factoryNativePluginDependency = "amcompat-probe-dependency"

var errFactoryPluginSkillAbsent = errors.New("Factory plugin skill absent")

// spec: plugin-delivery/reload-requirement-is-reported
func runFactoryNativePluginProbe(e *Env, binary string, f compatibilityFixture, scope string, row *compatibilityResult) error {
	pluginName := f.name("native-plugin")
	skillName := f.name("native-plugin-skill")
	pluginRoot := filepath.Join(e.workspace, "native-plugin")
	manifestPath := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
	skillPath := filepath.Join(pluginRoot, "skills", skillName, "SKILL.md")
	catalogPath := filepath.Join(e.workspace, "test-catalogs", "factory.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		return fmt.Errorf("prepare Factory plugin manifest: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		return fmt.Errorf("prepare Factory plugin skill: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o700); err != nil {
		return fmt.Errorf("prepare Factory catalog override: %w", err)
	}
	manifest := []byte(fmt.Sprintf(`{"name":%q,"version":"1.0.0"}`, pluginName))
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		return fmt.Errorf("write Factory plugin manifest: %w", err)
	}
	packageManifest := fmt.Sprintf(`{"name":%q,"private":true,"dependencies":{%q:"0.0.0"}}`, pluginName, factoryNativePluginDependency)
	if err := os.WriteFile(filepath.Join(pluginRoot, "package.json"), []byte(packageManifest), 0o600); err != nil {
		return fmt.Errorf("write Factory plugin dependency fixture: %w", err)
	}
	if err := os.WriteFile(catalogPath, []byte(nativePluginCatalog("factory")), 0o600); err != nil {
		return fmt.Errorf("write Factory probe catalog override: %w", err)
	}

	firstBody := "Factory native plugin generation one."
	secondBody := "Factory native plugin generation two."
	skillDocument := func(body string) string {
		return fmt.Sprintf("---\nname: %s\ndescription: Factory native plugin probe\n---\n%s\n", skillName, body)
	}
	writeSkill := func(body string) error {
		return os.WriteFile(skillPath, []byte(skillDocument(body)), 0o600)
	}
	if err := writeSkill(firstBody); err != nil {
		return fmt.Errorf("write Factory plugin skill: %w", err)
	}

	before := runCompatibilityProtocol(e, binary, "factory-skills", 30*time.Second)
	row.Commands = append(row.Commands, before)
	if _, err := requireFactoryPluginSkill(&before, e, pluginName, skillName, true); err != nil {
		return fmt.Errorf("Factory plugin negative control failed: %w", err)
	}
	row.Commands[len(row.Commands)-1].Observed = append(row.Commands[len(row.Commands)-1].Observed, fmt.Sprintf("plugin skill absent before sync; checked %q and %q", skillName, pluginName+":"+skillName))

	writeConfig := func(selected bool) {
		resources := "[]"
		if selected {
			resources = fmt.Sprintf("[{kind: plugin, name: %s, delivery: native}]", pluginName)
		}
		config := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"+
			"materialization: {mode: symlink}\nsources:\n"+
			"  native: {directory: %q, format: claude-plugin}\n"+
			"resources:\n  native: %s\n", pluginRoot, resources)
		if scope == "user" {
			e.WriteRepoConfig("apiVersion: agent-marketplace/v4\n")
			e.WriteGlobalConfig(config)
		} else {
			e.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			e.WriteRepoConfig(config)
		}
	}
	writeConfig(true)
	sync := runFactoryPluginSync(e, row, catalogPath)
	if sync.ExitCode != 0 {
		return fmt.Errorf("Factory native plugin sync failed")
	}
	startupDeadline := time.Now().Add(60 * time.Second)
	skillInstalled := false
	metadataSessions := 0
	var entry compatibilityNativeEntry
	var err error
	for attempt := 1; attempt <= 4; attempt++ {
		remaining := time.Until(startupDeadline)
		if remaining <= 0 {
			break
		}
		sessionTimeout := min(remaining, 30*time.Second)
		command := runCompatibilityProtocol(e, binary, "factory-skills", sessionTimeout)
		metadataSessions++
		entry, err = requireFactoryPluginSkill(&command, e, pluginName, skillName, false, firstBody, firstBody+"\n", skillDocument(firstBody))
		switch {
		case errors.Is(err, errFactoryPluginSkillAbsent):
			command.Observed = append(command.Observed, fmt.Sprintf("plugin skill absent in metadata session %d", attempt))
			row.Commands = append(row.Commands, command)
		case err != nil:
			switch {
			case entry.Name != "":
				command.Observed = append(command.Observed, fmt.Sprintf("plugin skill present in metadata session %d; matched name %q", attempt, entry.Name))
			case strings.Contains(err.Error(), "multiple matches"):
				command.Observed = append(command.Observed, fmt.Sprintf("multiple plugin skill matches in metadata session %d", attempt))
			default:
				command.Observed = append(command.Observed, fmt.Sprintf("plugin skill inspection failed in metadata session %d", attempt))
			}
			row.Commands = append(row.Commands, command)
			return fmt.Errorf("Factory plugin skill content after sync: %w", err)
		default:
			command.Observed = append(command.Observed, fmt.Sprintf("plugin skill present in metadata session %d; matched name %q", attempt, entry.Name))
			row.Commands = append(row.Commands, command)
			skillInstalled = true
		}
		if skillInstalled || attempt == 4 {
			break
		}
		remaining = time.Until(startupDeadline)
		if remaining <= 0 {
			break
		}
		pause := min(remaining, 5*time.Second)
		time.Sleep(pause)
	}
	recordFactoryPluginDiagnostics(e, scope, &row.Commands[len(row.Commands)-1])
	if !skillInstalled {
		return fmt.Errorf("Factory plugin skill absent after %d metadata sessions", metadataSessions)
	}
	if !filepath.IsAbs(entry.FilePath) || filepath.Base(entry.FilePath) != "SKILL.md" {
		return fmt.Errorf("Factory plugin skill inventory omitted its cached file path")
	}
	home, err := filepath.EvalSymlinks(e.home)
	if err != nil {
		return fmt.Errorf("resolve isolated Factory home: %w", err)
	}
	cachedSkill, err := filepath.EvalSymlinks(entry.FilePath)
	if err != nil {
		return fmt.Errorf("resolve cached Factory plugin skill: %w", err)
	}
	cacheRel, err := filepath.Rel(home, cachedSkill)
	if err != nil || cacheRel == "." || cacheRel == ".." || strings.HasPrefix(cacheRel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Factory plugin skill cache escaped the isolated home")
	}
	cacheRoot := filepath.Dir(filepath.Dir(filepath.Dir(entry.FilePath)))
	if _, err := os.Stat(filepath.Join(cacheRoot, "package.json")); err != nil {
		return fmt.Errorf("Factory plugin cache omitted the dependency fixture: %w", err)
	}
	dependencyPath := filepath.Join(cacheRoot, "node_modules", factoryNativePluginDependency)
	if _, err := os.Lstat(dependencyPath); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("Factory installed the plugin's npm dependency implicitly")
	}
	row.Commands[len(row.Commands)-1].Observed = append(row.Commands[len(row.Commands)-1].Observed, "exact generation-one content", "declared npm dependency not installed")
	listScope := "project"
	if scope == "user" {
		listScope = "user"
	}
	list := runCompatibilityCommand(e, binary, []string{"plugin", "list", "--scope", listScope}, 20*time.Second)
	row.Commands = append(row.Commands, list)
	if err := compatibilityInspectionError(list); err != nil {
		return fmt.Errorf("Factory installed-plugin inventory failed: %w", err)
	}
	if !strings.Contains(list.output, pluginName) {
		return fmt.Errorf("Factory installed-plugin inventory omitted the selected plugin")
	}

	if err := writeSkill(secondBody); err != nil {
		return fmt.Errorf("write updated Factory plugin skill: %w", err)
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil || string(afterManifest) != string(manifest) {
		return fmt.Errorf("Factory plugin authored version changed with its generation")
	}
	updatedSync := runFactoryPluginSync(e, row, catalogPath)
	if updatedSync.ExitCode != 0 {
		return fmt.Errorf("Factory native plugin generation update failed")
	}
	status := runFactoryPluginCommand(e, row, catalogPath, "status")
	if status.ExitCode != 0 {
		return fmt.Errorf("Factory plugin status failed")
	}
	if !strings.Contains(updatedSync.output, "reload-required") || !strings.Contains(status.output, "reload-required") {
		return fmt.Errorf("Factory generation update did not report reload-required")
	}
	refreshObserved := false
	for attempt := 1; attempt <= 2; attempt++ {
		command := runCompatibilityProtocol(e, binary, "factory-skills", 30*time.Second)
		entry, err := requireFactoryPluginSkill(&command, e, pluginName, skillName, false)
		if err != nil {
			row.Commands = append(row.Commands, command)
			return fmt.Errorf("Factory plugin skill after generation update: %w", err)
		}
		command.Observed = append(command.Observed, fmt.Sprintf("plugin skill matched name %q after generation update", entry.Name))
		switch entry.Content {
		case secondBody, secondBody + "\n", skillDocument(secondBody):
			command.Observed = append(command.Observed, fmt.Sprintf("generation refresh observed in metadata session %d", attempt))
			refreshObserved = true
		case firstBody, firstBody + "\n", skillDocument(firstBody):
			command.Observed = append(command.Observed, fmt.Sprintf("generation refresh not observed in metadata session %d", attempt))
		default:
			row.Commands = append(row.Commands, command)
			return fmt.Errorf("Factory skill content matched neither plugin generation")
		}
		row.Commands = append(row.Commands, command)
		if refreshObserved || attempt == 2 {
			break
		}
		time.Sleep(time.Second)
	}
	if !refreshObserved {
		row.Commands[len(row.Commands)-1].Observed = append(row.Commands[len(row.Commands)-1].Observed, "reload-required reported by sync and status")
		return fmt.Errorf("Factory generation remained at generation one in both metadata sessions after update sync; reload-required was reported by sync and status")
	}

	writeConfig(false)
	removed := runFactoryPluginSync(e, row, catalogPath)
	if removed.ExitCode != 0 {
		return fmt.Errorf("Factory native plugin removal sync failed")
	}
	afterRemoval := runCompatibilityProtocol(e, binary, "factory-skills", 30*time.Second)
	row.Commands = append(row.Commands, afterRemoval)
	if _, err := requireFactoryPluginSkill(&afterRemoval, e, pluginName, skillName, true); err != nil {
		return fmt.Errorf("Factory plugin remained in skill inventory after removal: %w", err)
	}
	row.Commands[len(row.Commands)-1].Observed = append(row.Commands[len(row.Commands)-1].Observed, fmt.Sprintf("plugin skill absent after removal sync; checked %q and %q", skillName, pluginName+":"+skillName))
	row.Reason += "; factory-native-plugin completed at " + scope + " scope"
	return nil
}

func requireFactoryPluginSkill(command *compatibilityCommand, e *Env, pluginName, name string, absent bool, expectedContent ...string) (compatibilityNativeEntry, error) {
	if err := compatibilityInspectionError(*command); err != nil {
		return compatibilityNativeEntry{}, err
	}
	entries, err := compatibilityProtocolInventory(command.output, "factory-skills", "skill", e.workspace)
	if err != nil {
		return compatibilityNativeEntry{}, err
	}
	var matches []compatibilityNativeEntry
	for _, entry := range entries {
		if entry.Name == name || entry.Name == pluginName+":"+name {
			matches = append(matches, entry)
		}
	}
	if len(matches) > 1 {
		return compatibilityNativeEntry{}, fmt.Errorf("native inventory contains multiple matches for the Factory plugin skill")
	}
	if len(matches) == 1 && matches[0].Content == "" {
		return matches[0], fmt.Errorf("Factory plugin skill present without content")
	}
	if absent {
		if len(matches) != 0 {
			return compatibilityNativeEntry{}, fmt.Errorf("native inventory already contains the Factory plugin skill")
		}
		command.Observed = append(command.Observed, "absent: "+name+" or "+pluginName+":"+name)
		return compatibilityNativeEntry{}, nil
	}
	if len(matches) == 0 {
		return compatibilityNativeEntry{}, errFactoryPluginSkillAbsent
	}
	if len(expectedContent) > 0 {
		for _, want := range expectedContent {
			if matches[0].Content == want {
				return matches[0], nil
			}
		}
		return compatibilityNativeEntry{}, fmt.Errorf("native Factory plugin skill content differs from the requested generation")
	}
	return matches[0], nil
}

func recordFactoryPluginDiagnostics(e *Env, scope string, command *compatibilityCommand) {
	pluginStatePath := filepath.Join(e.home, ".factory", "plugins")
	entries, err := os.ReadDir(pluginStatePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		command.Observed = append(command.Observed, "Droid plugin state under ~/.factory/plugins: absent")
	case err != nil:
		command.Observed = append(command.Observed, "Droid plugin state under ~/.factory/plugins: unavailable")
	default:
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		command.Observed = append(command.Observed, "Droid plugin state under ~/.factory/plugins: ["+strings.Join(names, ", ")+"]")
	}

	registrationPath := filepath.Join(e.home, ".factory", "settings.json")
	if scope != "user" {
		registrationPath = filepath.Join(e.workspace, ".factory", "settings.local.json")
	}
	data, err := os.ReadFile(registrationPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		command.Observed = append(command.Observed, "Factory registration keys: extraKnownMarketplaces=false, enabledPlugins=false")
	case err != nil:
		command.Observed = append(command.Observed, "Factory registration keys: unavailable")
	default:
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(data, &settings); err != nil || settings == nil {
			command.Observed = append(command.Observed, "Factory registration keys: unavailable (invalid settings JSON)")
			return
		}
		_, hasMarketplaces := settings["extraKnownMarketplaces"]
		_, hasEnabledPlugins := settings["enabledPlugins"]
		command.Observed = append(command.Observed, fmt.Sprintf("Factory registration keys: extraKnownMarketplaces=%t, enabledPlugins=%t", hasMarketplaces, hasEnabledPlugins))
	}
}

func TestRequireFactoryPluginSkillMatchesNamespacedName(t *testing.T) {
	e := &Env{workspace: t.TempDir()}
	inventory := `{"skills":[{"name":"plugin:skill","content":"fixture"}]}`

	t.Run("presence", func(t *testing.T) {
		command := compatibilityCommand{output: inventory}
		entry, err := requireFactoryPluginSkill(&command, e, "plugin", "skill", false, "fixture")
		if err != nil {
			t.Fatalf("namespaced Factory skill was not matched: %v", err)
		}
		if entry.Name != "plugin:skill" {
			t.Fatalf("matched Factory skill name = %q, want %q", entry.Name, "plugin:skill")
		}
	})

	t.Run("absence", func(t *testing.T) {
		command := compatibilityCommand{output: inventory}
		if _, err := requireFactoryPluginSkill(&command, e, "plugin", "skill", true); err == nil {
			t.Fatal("namespaced Factory skill was accepted as absent")
		}
	})
}

func runFactoryPluginSync(e *Env, row *compatibilityResult, catalogPath string) compatibilityCommand {
	return runFactoryPluginCommand(e, row, catalogPath, "sync", "--no-backup")
}

func runFactoryPluginCommand(e *Env, row *compatibilityResult, catalogPath string, args ...string) compatibilityCommand {
	result := e.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_PLUGIN_CATALOG": catalogPath}, args...)
	command := compatibilityCommand{
		Args:        append([]string{e.binary}, args...),
		ExitCode:    result.ExitCode,
		OutputBytes: len(result.Combined),
		Diagnostics: compatibilityDiagnostics(result.Combined),
		output:      result.Combined,
	}
	if result.ExitCode != 0 {
		command.Error = "Marketplace command exited unsuccessfully"
	}
	command.Observed = append(command.Observed, "Factory probe "+strings.Join(args, " ")+" completed")
	row.Commands = append(row.Commands, command)
	return command
}
