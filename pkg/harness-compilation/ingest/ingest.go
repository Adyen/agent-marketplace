// Package ingest provides the ingestion layer for reading AI-agent configuration
// resources from harness-specific source layouts. Each supported harness registers
// an Ingester that can detect and load its layout from an fs.FS.
package ingest

import (
	"fmt"
	"io/fs"
	"log/slog"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Harness identifies a source harness format. It is owned by the shared
// target registry so source and destination identities cannot drift.
type Harness = targetmeta.Target

const (
	HarnessClaude   = targetmeta.TargetClaude
	HarnessFactory  = targetmeta.TargetFactory
	HarnessCursor   = targetmeta.TargetCursor
	HarnessWindsurf = targetmeta.TargetWindsurf
	HarnessGemini   = targetmeta.TargetGemini
	HarnessDevin    = targetmeta.TargetDevin
	HarnessCodex    = targetmeta.TargetCodex
)

// Bundle holds the result of ingesting a harness-specific source layout:
// a set of IR resources plus any diagnostics produced during ingestion.
type Bundle struct {
	Manifest    ir.Manifest       // synthetic for harnesses without a manifest
	Resources   []ir.Resource     // loaded IR resources
	Diagnostics []diag.Diagnostic // ingest-side diagnostics (code prefix "ingest.*")
}

// Manifest describes the bundle metadata. For harnesses that have a manifest
// file (e.g. .claude-plugin/plugin.json), it is populated from that file.
// For workspace-form layouts, it is synthesized.
type Manifest = ir.Manifest

// Ingester is the interface that each source harness must implement.
type Ingester interface {
	// Source returns the harness that this ingester handles.
	Source() Harness

	// Detect reports whether the given filesystem contains a layout
	// that this ingester can handle.
	Detect(src fs.FS) bool

	// LoadBundle reads all resources from the given filesystem and returns
	// them as an IR Bundle.
	LoadBundle(src fs.FS) (*Bundle, error)
}

// registry holds the set of registered ingesters, keyed by Harness.
var registry = map[Harness]Ingester{}

// Register adds an Ingester to the global registry. It panics if an ingester
// is already registered for the same Harness.
func Register(ing Ingester) {
	h := ing.Source()
	if _, exists := registry[h]; exists {
		panic(fmt.Sprintf("ingest: ingester already registered for harness %q", h))
	}
	registry[h] = ing
}

// For returns the Ingester registered for the given harness, if any.
func For(h Harness) (Ingester, bool) {
	ing, ok := registry[h]
	return ing, ok
}

// RegisteredSources returns the harnesses that currently have a registered
// ingester, sorted by name. It is a read-only view for startup diagnostics and
// registration-completeness tests; callers must not mutate the registry.
func RegisteredSources() []Harness {
	sources := make([]Harness, 0, len(registry))
	for h := range registry {
		sources = append(sources, h)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i] < sources[j] })
	return sources
}

// Autodetect probes the filesystem for harness-specific layout markers
// and returns the detected harness. Returns ("", false) if no harness
// layout is detected.
//
// Detection order (most-specific first):
//  0. .devin/ workspace markers → Devin (see devinWinsOverCompatibilityLayouts)
//  1. .claude-plugin/plugin.json → Claude
//  2. .factory-plugin/plugin.json → Factory
//  3. .cursor-plugin/plugin.json → Cursor
//  4. gemini-extension.json → Gemini
//  5. .codex/ → Codex
//  6. .claude/ with skills/agents/commands/hooks/.mcp.json → Claude
//  7. .factory/ → Factory
//  8. .cursor/ → Cursor
//  9. .gemini/ → Gemini
//  10. .windsurf/ → Windsurf
//  11. Bare skills directory (subdirs with SKILL.md, or SKILL.md at root) → Claude
func Autodetect(src fs.FS) (Harness, bool) {
	return autodetectInternal(src, false)
}

// AutodetectStrict is like Autodetect but excludes the "bare skills directory"
// detection form. This is used by sub-bundle scanning where individual skill
// directories should not be treated as independent bundle roots.
func AutodetectStrict(src fs.FS) (Harness, bool) {
	return autodetectInternal(src, true)
}

// devinWinsOverCompatibilityLayouts documents why Devin is probed before every
// other marker, including the explicit plugin manifests below.
//
// The promoted Devin plan states the rule twice. "Bidirectional conversion":
// "When `.devin` and compatibility layouts coexist at the explicit root,
// autodetection selects Devin; callers may explicitly choose another source
// harness." Evidence baseline, "Compatibility imports": "Devin can read
// resources from `.agents`, Cursor, Windsurf, Claude, Copilot, OpenCode, VS
// Code, and Zed layouts [...] Autodetection prefers `.devin` when it coexists
// with compatibility markers."
//
// Every other marker this function checks — a Claude/Factory/Cursor plugin
// manifest, a Gemini extension manifest, `.claude/`, `.factory/`, `.cursor/`,
// `.gemini/`, `.windsurf/`, a bare skills directory — is either one of those
// compatibility layouts or a sibling harness's own layout. A canonical `.devin`
// directory, by contrast, can only have been written for Devin. So the presence
// of a `.devin` marker is the single most specific signal available, and it is
// checked first rather than being slotted into autodetectOrder, which runs
// AFTER the explicit manifest markers.
//
// Determinism: the Devin ingester's Detect reads a fixed, ordered list of
// literal marker paths, so the winner does not depend on the order fs.ReadDir
// happens to return entries in. TestAutodetectDevinPrecedenceIsOrderIndependent
// pins that against a workspace carrying several candidate layouts at once.
//
// Only the WORKSPACE shape participates. A direct `~/.config/devin`-shaped root
// is never autodetected — see the Devin ingester's Detect.
func devinWinsOverCompatibilityLayouts(src fs.FS) bool {
	ing, ok := registry[HarnessDevin]
	return ok && ing.Detect(src)
}

func autodetectInternal(src fs.FS, strict bool) (Harness, bool) {
	if devinWinsOverCompatibilityLayouts(src) {
		return logAutodetectedHarness(HarnessDevin)
	}

	// Prefer explicit manifest markers before asking ingesters, so root-level
	// resource directories do not overshadow a more specific harness marker.
	explicit := []struct {
		path    string
		harness Harness
	}{
		{".claude-plugin/plugin.json", HarnessClaude},
		{".factory-plugin/plugin.json", HarnessFactory},
		{".cursor-plugin/plugin.json", HarnessCursor},
		{"gemini-extension.json", HarnessGemini},
	}
	for _, candidate := range explicit {
		if _, err := fs.ReadFile(src, candidate.path); err == nil {
			return logAutodetectedHarness(candidate.harness)
		}
	}

	// Check in priority order.
	for _, h := range autodetectOrder {
		ing, ok := registry[h]
		if !ok {
			continue
		}
		// In strict mode, prefer DetectStrict for ingesters that implement
		// it (e.g. Claude which has a permissive bare-skills-dir fallback).
		// Ingesters without DetectStrict use the standard Detect method —
		// they have no permissive forms that need filtering.
		if strict {
			if strictIng, ok := ing.(StrictDetector); ok {
				if strictIng.DetectStrict(src) {
					return logAutodetectedHarness(h)
				}
				continue
			}
		}
		if ing.Detect(src) {
			return logAutodetectedHarness(h)
		}
	}
	return "", false
}

func logAutodetectedHarness(harness Harness) (Harness, bool) {
	slog.Debug("autodetected harness", "harness", harness)
	return harness, true
}

// StrictDetector is an optional interface that ingesters can implement to
// provide a stricter detection mode that excludes permissive fallback forms
// (e.g. bare skills directories). Used by AutodetectStrict for sub-bundle
// scanning where individual skill directories must not be treated as
// independent bundle roots.
type StrictDetector interface {
	DetectStrict(src fs.FS) bool
}

// autodetectOrder defines the priority order for harness detection. Keep the
// priority separate from target display order because explicit markers are
// intentionally checked in a different, more-specific order above.
//
// Devin is deliberately absent: it is probed BEFORE the explicit manifest
// markers by devinWinsOverCompatibilityLayouts, which is strictly earlier than
// anything this list can express.
var autodetectOrder = []Harness{
	HarnessCodex,
	HarnessClaude,
	HarnessFactory,
	HarnessCursor,
	HarnessGemini,
	HarnessWindsurf,
}
