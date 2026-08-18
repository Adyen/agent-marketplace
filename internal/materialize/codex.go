package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

const (
	codexHooksFile = "hooks.json"
	codexMCPFile   = "config.toml"
)

type codexMCPEntry struct {
	name, resource string
	raw            []byte
	valueDigest    string
}

func (e codexMCPEntry) id() ownership.EntryID {
	return ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcp_servers", Key: e.name, IdentityDigest: ownership.Digest([]byte(e.name))}
}

func (e codexMCPEntry) owned() ownership.Entry {
	id := e.id()
	return ownership.Entry{Kind: id.Kind, Container: id.Container, Key: id.Key, IdentityDigest: id.IdentityDigest, ValueDigest: e.valueDigest, Resource: e.resource}
}

// splitCodexOwnedArtifacts keeps ownership-reconciled files out of the generic
// marker merge path.
func splitCodexOwnedArtifacts(artifacts []artifact.Artifact, resource string) ([]artifact.Artifact, []devinEntry, []codexMCPEntry, error) {
	var rest []artifact.Artifact
	var hooks []devinEntry
	var mcps []codexMCPEntry
	for _, candidate := range artifacts {
		if !candidate.ExternalReconciliationRequired {
			rest = append(rest, candidate)
			continue
		}
		switch candidate.Path {
		case ".codex/hooks.json":
			parsed, err := devinFragmentEntries(codexHooksFile, candidate.Bytes, resource)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("decode Codex hooks fragment: %w", err)
			}
			hooks = append(hooks, parsed...)
		case ".codex/config.toml":
			parsed, err := codexDesiredMCP(candidate.Bytes, resource)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("decode Codex MCP fragment: %w", err)
			}
			mcps = append(mcps, parsed...)
		default:
			rest = append(rest, candidate)
		}
	}
	return rest, hooks, mcps, nil
}

func codexDesiredMCP(raw []byte, resource string) ([]codexMCPEntry, error) {
	sections, _, err := codexMCPSections(raw)
	if err != nil {
		return nil, err
	}
	entries := make([]codexMCPEntry, 0, len(sections))
	for name, section := range sections {
		fragment := append([]byte(nil), raw[section.start:section.end]...)
		if len(fragment) > 0 && fragment[len(fragment)-1] != '\n' {
			fragment = append(fragment, '\n')
		}
		entries = append(entries, codexMCPEntry{name: name, resource: resource, raw: fragment, valueDigest: section.value.ValueDigest})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}

func codexMCPValues(raw []byte) (map[string]any, error) {
	var document map[string]any
	if err := toml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	value, ok := document["mcp_servers"]
	if !ok {
		return map[string]any{}, nil
	}
	servers, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp_servers is not a table")
	}
	return servers, nil
}

type codexMCPSection struct {
	start, end int
	value      ownership.EntryValue
}

func codexMCPSections(raw []byte) (map[string]codexMCPSection, []ownership.EntryValue, error) {
	values, err := codexMCPValues(raw)
	if err != nil {
		return nil, nil, err
	}
	sections := make(map[string]codexMCPSection, len(values))
	var parser unstable.Parser
	parser.Reset(raw)
	current := ""
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table:
			var keys []string
			it := node.Key()
			for it.Next() {
				keys = append(keys, string(it.Node().Data))
			}
			current = ""
			if len(keys) < 2 || keys[0] != "mcp_servers" {
				continue
			}
			current = keys[1]
			first := node.Child().Raw.Offset
			start := bytes.LastIndexByte(raw[:first], '\n') + 1
			section := sections[current]
			if section.end == 0 {
				section.start = start
			}
			section.end = lineEnd(raw, int(first))
			sections[current] = section
		case unstable.KeyValue:
			if current == "" {
				continue
			}
			section := sections[current]
			section.end = lineEnd(raw, int(node.Value().Raw.Offset+node.Value().Raw.Length))
			sections[current] = section
		}
	}
	if err := parser.Error(); err != nil {
		return nil, nil, err
	}
	existing := make([]ownership.EntryValue, 0, len(values))
	for name, value := range values {
		section, ok := sections[name]
		if !ok {
			return nil, nil, fmt.Errorf("MCP server %q is not represented by a table section", name)
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return nil, nil, err
		}
		id := ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcp_servers", Key: name, IdentityDigest: ownership.Digest([]byte(name))}
		section.value = ownership.EntryValue{ID: id, ValueDigest: ownership.Digest(canonical)}
		sections[name] = section
		existing = append(existing, section.value)
	}
	return sections, existing, nil
}

func lineEnd(raw []byte, offset int) int {
	if offset > len(raw) {
		offset = len(raw)
	}
	if next := bytes.IndexByte(raw[offset:], '\n'); next >= 0 {
		return offset + next + 1
	}
	return len(raw)
}

func reconcileCodexMCP(ctx context.Context, report *Report, key outputKey, root string, desired []codexMCPEntry, dryRun bool) *Report {
	abs := filepath.Join(root, codexMCPFile)
	if len(desired) == 0 {
		if _, err := os.Lstat(abs); os.IsNotExist(err) {
			return report
		}
	}
	store, err := ownership.Open(root)
	if err != nil {
		return codexMCPDiag(report, key, err)
	}
	reconcile := func() error {
		if !dryRun {
			if _, err := store.Recover(); err != nil {
				return err
			}
		}
		raw, readErr := os.ReadFile(abs)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if os.IsNotExist(readErr) {
			raw = nil
		}
		sections, existing, err := codexMCPSections(raw)
		if err != nil {
			if readErr == nil && !dryRun {
				_, _ = store.BackupOnly(codexMCPFile, abs, ownership.ReasonSync)
			}
			return err
		}
		proof, _ := store.Proof(codexMCPFile)
		desiredValues := make([]ownership.EntryValue, 0, len(desired))
		byName := make(map[string]codexMCPEntry, len(desired))
		for _, entry := range desired {
			desiredValues = append(desiredValues, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest})
			byName[entry.name] = entry
		}
		drift := ownership.Classify(proof, existing, desiredValues)
		collisions := make(map[string]bool, len(drift.Collisions))
		for _, id := range drift.Collisions {
			collisions[id.Key] = true
			report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeOwnershipUnmanagedKey, Severity: diag.SeverityError, Target: string(key.target), Resource: byName[id.Key].resource, Message: fmt.Sprintf("Codex MCP server %q collides with an unmanaged config.toml entry; existing entry preserved", id.Key)})
		}
		type edit struct {
			start, end  int
			replacement []byte
		}
		var edits []edit
		for _, id := range drift.Stale {
			if section, ok := sections[id.Key]; ok {
				edits = append(edits, edit{start: section.start, end: section.end})
			}
		}
		owned := make([]ownership.Entry, 0, len(desired))
		for _, entry := range desired {
			if collisions[entry.name] {
				continue
			}
			owned = append(owned, entry.owned())
			section, exists := sections[entry.name]
			if exists && proof.Classify(entry.id(), section.value.ValueDigest) == ownership.StatusOwnedClean && section.value.ValueDigest == entry.valueDigest {
				continue
			}
			if exists {
				edits = append(edits, edit{start: section.start, end: section.end, replacement: entry.raw})
			} else {
				prefix := []byte(nil)
				if len(raw) > 0 && raw[len(raw)-1] != '\n' {
					prefix = []byte("\n")
				}
				if len(raw) > 0 {
					prefix = append(prefix, '\n')
				}
				edits = append(edits, edit{start: len(raw), end: len(raw), replacement: append(prefix, entry.raw...)})
			}
		}
		sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		next := append([]byte(nil), raw...)
		for _, e := range edits {
			patched := make([]byte, 0, len(next)-(e.end-e.start)+len(e.replacement))
			patched = append(patched, next[:e.start]...)
			patched = append(patched, e.replacement...)
			patched = append(patched, next[e.end:]...)
			next = patched
		}
		if bytes.Equal(raw, next) {
			return nil
		}
		if dryRun {
			codexMCPMarkWritten(report, key, root)
			return nil
		}
		gen, digest := devinPriorForApply(raw, readErr, proof)
		_, err = store.Apply(ownership.Mutation{Rel: codexMCPFile, AbsFile: abs, NextBytes: next, Entries: owned, Reason: ownership.ReasonSync, ExpectPriorGeneration: gen, ExpectPriorDigest: digest})
		if err == nil {
			slog.Info("Codex MCP configuration reconciled", "target", key.target, "machine_scope", key.machineScope, "file", abs, "owned_entries", len(owned))
			codexMCPMarkWritten(report, key, root)
		}
		return err
	}
	if dryRun {
		err = reconcile()
	} else {
		err = store.WithWriteSessionContext(ctx, "reconcile-codex-mcp", reconcile)
	}
	if err != nil {
		return codexMCPDiag(report, key, err)
	}
	return report
}

func codexMCPMarkWritten(report *Report, key outputKey, root string) {
	tr := TargetReport{Target: key.target, MachineScope: key.machineScope, OutDir: root, Written: []string{codexMCPFile}}
	report.PerRoot = append(report.PerRoot, tr)
	report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], tr)
}

func codexMCPDiag(report *Report, key outputKey, err error) *Report {
	report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.codex.aggregate-malformed", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("reconcile Codex config.toml: %v", err)})
	return report
}
