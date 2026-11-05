package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Satisfy the pinned native loader's dependency preflight with inert fixtures.
// No package is installed, imported, or acquired by this inspection gate.
func prepareOpenCodeCompatibilityRuntime(e *Env) error {
	files := map[string]string{
		filepath.Join(e.home, "opencode-models.json"): "{}",
	}
	for _, root := range []string{filepath.Join(e.workspace, ".opencode"), filepath.Join(e.home, ".config", "opencode")} {
		files[filepath.Join(root, "package.json")] = `{"dependencies":{"@opencode-ai/plugin":"1.18.34"}}`
		files[filepath.Join(root, "package-lock.json")] = `{"lockfileVersion":3,"packages":{"":{"dependencies":{"@opencode-ai/plugin":"1.18.34"}}}}`
		if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o700); err != nil {
			return err
		}
	}
	for path, content := range files {
		data, err := os.ReadFile(path)
		if err == nil {
			if !bytes.Equal(data, []byte(content)) {
				return fmt.Errorf("isolated OpenCode dependency fixture changed")
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func requireOpenCodeInventory(command *compatibilityCommand, f compatibilityFixture, probe compatibilityNativeProbe, absent bool) error {
	if err := compatibilityInspectionError(*command); err != nil {
		return err
	}
	data := command.inventory
	if data == nil {
		data = []byte(command.output)
	}
	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	inventory := make(map[string]entry)
	switch probe.kind {
	case "skill":
		var skills []entry
		if err := json.Unmarshal(data, &skills); err != nil || skills == nil {
			return fmt.Errorf("native skill inventory is not a JSON array")
		}
		for _, skill := range skills {
			inventory[skill.Name] = skill
		}
	case "command":
		var configuration struct {
			Commands map[string]entry `json:"command"`
		}
		if err := json.Unmarshal(data, &configuration); err != nil {
			return fmt.Errorf("native command inventory is not a JSON object")
		}
		inventory = configuration.Commands
	default:
		return fmt.Errorf("undeclared OpenCode JSON inventory")
	}
	for _, name := range probe.names {
		value, exists := inventory[name]
		if exists == absent {
			return fmt.Errorf("expected native %s identity present=%t", probe.kind, !absent)
		}
		if !absent && value.Description != f.description(probe.kind) {
			return fmt.Errorf("native resource description differs from fixture")
		}
		observation := name
		if absent {
			observation = "absent: " + name
		}
		command.Observed = append(command.Observed, observation)
	}
	return nil
}
