// Package hookreview renders hook publication reports for CLI and TUI review.
package hookreview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/materialize"
)

// Write renders the completed or pending hook changes for user review.
func Write(out io.Writer, changes []materialize.HookChange, homeDir string, pending bool) error {
	if len(changes) == 0 {
		return nil
	}
	changes = slices.Clone(changes)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Target != changes[j].Target {
			return changes[i].Target < changes[j].Target
		}
		if changes[i].MachineScope != changes[j].MachineScope {
			return !changes[i].MachineScope
		}
		if changes[i].File != changes[j].File {
			return changes[i].File < changes[j].File
		}
		if changes[i].Event != changes[j].Event {
			return changes[i].Event < changes[j].Event
		}
		if changes[i].Matcher != changes[j].Matcher {
			return changes[i].Matcher < changes[j].Matcher
		}
		if !bytes.Equal(changes[i].Before, changes[j].Before) {
			return bytes.Compare(changes[i].Before, changes[j].Before) < 0
		}
		return bytes.Compare(changes[i].After, changes[j].After) < 0
	})
	header := "Hook changes:"
	if pending {
		header = "Hook changes (pending, not written):"
	}
	if _, err := fmt.Fprintln(out, header); err != nil {
		return err
	}
	removed := false
	for _, change := range changes {
		scope := "repository"
		if change.MachineScope {
			scope = "machine"
		}
		action := "replace"
		switch {
		case len(change.Before) == 0:
			action = "add"
		case len(change.After) == 0:
			action = "remove"
			removed = true
		}
		if _, err := fmt.Fprintf(out, "  %s (%s) %s %s: %s", change.Target, scope, action, displayText(abbreviateHome(change.File, homeDir)), displayText(change.Event)); err != nil {
			return err
		}
		if change.Matcher != "" {
			if _, err := fmt.Fprintf(out, " matcher %s", strconv.Quote(change.Matcher)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		for _, side := range []struct {
			label string
			raw   json.RawMessage
		}{{"-", change.Before}, {"+", change.After}} {
			for _, line := range hookChangePayloadLines(side.raw) {
				if _, err := fmt.Fprintf(out, "    %s %s\n", side.label, line); err != nil {
					return err
				}
			}
		}
	}
	if pending && removed {
		if _, err := fmt.Fprintln(out, "Deselecting hooks removes their owned entries and owned support files."); err != nil {
			return err
		}
	}
	return nil
}

func displayText(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

func hookChangePayloadLines(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil
	}
	handlers := []map[string]json.RawMessage{entry}
	if group, ok := entry["hooks"]; ok {
		var inner []map[string]json.RawMessage
		if err := json.Unmarshal(group, &inner); err == nil && len(inner) > 0 {
			handlers = inner
		}
	}
	var lines []string
	for _, handler := range handlers {
		matched := false
		for _, field := range []string{"command", "prompt"} {
			value, ok := handler[field]
			if !ok {
				continue
			}
			var s string
			if err := json.Unmarshal(value, &s); err != nil {
				continue
			}
			matched = true
			lines = append(lines, fmt.Sprintf("%s: %s", field, strconv.Quote(s)))
		}
		if !matched {
			compact, err := json.Marshal(handler)
			if err != nil {
				continue
			}
			lines = append(lines, string(compact))
		}
	}
	return lines
}

func abbreviateHome(dir, homeDir string) string {
	if homeDir != "" && strings.HasPrefix(dir, homeDir+string(filepath.Separator)) {
		return "~" + dir[len(homeDir):]
	}
	return dir
}
