package materialize

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// ErrRepairCandidateStale is returned when an authorized candidate no longer
// matches what the preview showed.
var ErrRepairCandidateStale = errors.New("materialize: repair candidate no longer matches the previewed state")

// ErrRepairGenerationInFlight is returned when a repair action would overwrite
// an incomplete ownership journal. A normal sync must recover it first.
var ErrRepairGenerationInFlight = errors.New("materialize: an interrupted ownership generation is still in flight for this target root; run `agent-marketplace sync` to recover it before repairing")

// AdoptionCandidate is one exact adoption match and its marketplace resource.
type AdoptionCandidate struct {
	Candidate ownership.AdoptionCandidate
	Resource  string
}

func (c AdoptionCandidate) EntryKind() string { return string(c.Candidate.EntryID().Kind) }
func (c AdoptionCandidate) Identity() string {
	return ownedAggregateIdentityLabel(c.Candidate.EntryID())
}

// ConflictCandidate is one unmanaged entry whose identity matches desired
// output but whose value differs.
type ConflictCandidate struct {
	Candidate ownership.ConflictCandidate
	Resource  string
}

func (c ConflictCandidate) EntryKind() string { return string(c.Candidate.EntryID().Kind) }
func (c ConflictCandidate) Identity() string {
	return ownedAggregateIdentityLabel(c.Candidate.EntryID())
}
func (c ConflictCandidate) ExistingValueDigest() string {
	return c.Candidate.ExistingValueDigest()
}
func (c ConflictCandidate) DesiredValueDigest() string { return c.Candidate.DesiredValueDigest() }
func (c ConflictCandidate) Differs() bool              { return c.Candidate.Differs() }
func (c ConflictCandidate) Difference() string {
	return fmt.Sprintf("existing value digest %s, generated value digest %s",
		c.Candidate.ExistingValueDigest(), c.Candidate.DesiredValueDigest())
}

// OrphanCandidate is one evidence-backed orphan nomination.
type OrphanCandidate struct {
	Candidate ownership.OrphanCandidate
}

func (c OrphanCandidate) EntryKind() string { return string(c.Candidate.EntryID().Kind) }
func (c OrphanCandidate) Identity() string  { return ownedAggregateIdentityLabel(c.Candidate.EntryID()) }
func (c OrphanCandidate) Evidence() string  { return c.Candidate.Evidence().Summary() }

// RepairFile is the repair preview for one ownership-reconciled aggregate file,
// plus the target adapter and handles needed to act on it.
type RepairFile struct {
	Target       hcc.Target
	MachineScope bool
	OutDir       string
	Rel          string

	Adoptions []AdoptionCandidate
	Orphans   []OrphanCandidate
	Conflicts []ConflictCandidate

	ExistingEntries int
	DesiredEntries  int

	store   *ownership.Store
	adapter repairFileAdapter
	desired []repairDesiredEntry
}

func (f *RepairFile) AbsFile() string { return filepath.Join(f.OutDir, f.Rel) }

// RepairPreview is the read-only repair picture for one target.
type RepairPreview struct {
	Files []*RepairFile
	Diags []diag.Diagnostic
}

func (p *RepairPreview) Actionable() bool {
	for _, file := range p.Files {
		if len(file.Adoptions) > 0 || len(file.Orphans) > 0 || len(file.Conflicts) > 0 {
			return true
		}
	}
	return false
}

func (p *RepairPreview) HasErrors() bool {
	for _, diagnostic := range p.Diags {
		if diagnostic.Severity == diag.SeverityError {
			return true
		}
	}
	return false
}

func ownedAggregateIdentityLabel(id ownership.EntryID) string {
	if id.Key != "" {
		return id.Key
	}
	return id.Container
}

func repairDiagnosticSummary(diagnostics []diag.Diagnostic) string {
	if len(diagnostics) == 0 {
		return "no diagnostics"
	}
	parts := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", diagnostic.Code, diagnostic.Message))
		} else {
			parts = append(parts, diagnostic.Message)
		}
	}
	return strings.Join(parts, "; ")
}

func repairDiagnosticCodeSummary(diagnostics []diag.Diagnostic) string {
	if len(diagnostics) == 0 {
		return "no diagnostics"
	}
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "" {
			codes = append(codes, "unclassified diagnostic")
			continue
		}
		codes = append(codes, diagnostic.Code)
	}
	return strings.Join(codes, "; ")
}

type repairDesiredEntry struct {
	value    ownership.EntryValue
	raw      []byte
	resource string
}

type repairAggregateState struct {
	raw      []byte
	existing []ownership.EntryValue
	proof    ownership.Proof
	remove   func(ownership.EntryID, string) ([]byte, error)
	replace  func(ownership.EntryID, []byte) ([]byte, error)
}

type repairFileAdapter interface {
	read(*ownership.Store, outputKey, string, string) (*repairAggregateState, []diag.Diagnostic)
	replaceable(ownership.EntryID) bool
	diagnosticSummary([]diag.Diagnostic) string
}

func repairDesiredValues(entries []repairDesiredEntry) []ownership.EntryValue {
	values := make([]ownership.EntryValue, 0, len(entries))
	for _, entry := range entries {
		values = append(values, entry.value)
	}
	return values
}

// TargetRepairPreview pairs one ownership-reconciled target with its read-only
// repair preview.
type TargetRepairPreview struct {
	Target  hcc.Target
	Preview *RepairPreview
}

// RepairPreviews enumerates the materialization adapters in stable target order.
func RepairPreviews(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact) ([]TargetRepairPreview, error) {
	registry, err := harness.NewInstallationAdapterRegistry()
	if err != nil {
		return nil, err
	}
	targets := registry.Targets()
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	bindings := materializeAdapters()
	out := make([]TargetRepairPreview, 0, len(targets))
	for _, target := range targets {
		adapter, ok := registry.Lookup(target)
		if !ok {
			continue
		}
		binding, ok := bindings[adapter.AggregateHandlerKey()]
		if !ok || binding.repair == nil || binding.repairKey != adapter.RepairKey() {
			continue
		}
		preview, err := binding.repair(ctx, repoRoot, resolved)
		if err != nil {
			return nil, err
		}
		out = append(out, TargetRepairPreview{Target: target, Preview: preview})
	}
	return out, nil
}
