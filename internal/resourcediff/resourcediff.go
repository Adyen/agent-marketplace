// Package resourcediff renders normalized review views of selected agent
// resources and compares two effective selected-resource sets. It is pure:
// callers hand it resolved resources with their exact admitted supporting
// bytes, and it classifies additions, removals, source changes, content
// modifications, supporting-file changes, and revision/evidence-only changes.
//
// The comparison boundary is the resolver's effective selected set, never a
// whole registry repository, installed files, or generated harness output.
// Resource identity keeps scope, source, ref, path, and coordinate distinct so
// complementary occurrences and independent same-named hooks are never merged
// by resource name alone.
package resourcediff

import (
	"fmt"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Identity identifies one selected resource occurrence across two resolution
// views. All fields except Via participate in matching; Via records the
// collection a resource came through and is informational only.
type Identity struct {
	Scope      string // owning configuration layer: user, repo, local, organization
	Kind       string // normalized resource kind
	Name       string // installed name
	SourceURL  string // canonical Git registry URL or Marketplace endpoint
	InstanceID string // Marketplace instance identity (empty for Git)
	Coordinate string // Marketplace registry coordinate (empty for Git)
	Ref        string // Git ref (empty for Marketplace)
	Path       string // registry root path (empty for root or Marketplace)
	Via        string // "collection:<name>@<version>" provenance, informational
}

func (id Identity) matchKey() string {
	return id.Scope + "\x00" + id.Kind + "\x00" + id.Name + "\x00" + id.SourceURL + "\x00" + id.InstanceID + "\x00" + id.Coordinate + "\x00" + id.Ref + "\x00" + id.Path
}

func (id Identity) occurrenceKey() string {
	return id.Scope + "\x00" + id.Kind + "\x00" + id.Name
}

func (id Identity) sortKey() string {
	return id.Scope + "\x00" + id.SourceURL + "\x00" + id.InstanceID + "\x00" + id.Coordinate + "\x00" + id.Ref + "\x00" + id.Path + "\x00" + id.Kind + "\x00" + id.Name
}

// SupportFile is one admitted supporting file. Data carries the exact bytes
// when the caller retained bodies for diff rendering; a summary-only
// comparison sets Digest alone so changes are detected without acquiring or
// holding file bodies.
type SupportFile struct {
	Data       []byte
	Digest     string
	Executable bool
}

// Side is one resource occurrence on one side of the comparison. Resource is
// the canonical IR definition; Revision is the exact revision identity (Git
// commit or Marketplace content digest).
type Side struct {
	Identity Identity
	Revision string
	Resource ir.Resource
	Support  map[string]SupportFile
}

// ChangeKind classifies one resource-level change record.
type ChangeKind int

const (
	// ChangeAdded reports a resource selected only in the candidate set.
	ChangeAdded ChangeKind = iota
	// ChangeRemoved reports a resource selected only in the prior set.
	ChangeRemoved
	// ChangeSource reports a changed collision winner: the same scope, kind,
	// and name resolves from a different source.
	ChangeSource
	// ChangeModified reports changed reviewed content (definition or
	// supporting files) for the same occurrence identity.
	ChangeModified
	// ChangeRevisionOnly reports a revision/evidence change with unchanged
	// reviewed content.
	ChangeRevisionOnly
)

func (k ChangeKind) String() string {
	switch k {
	case ChangeAdded:
		return "added"
	case ChangeRemoved:
		return "removed"
	case ChangeSource:
		return "source-changed"
	case ChangeModified:
		return "modified"
	case ChangeRevisionOnly:
		return "revision-only"
	default:
		return "unknown"
	}
}

// SupportChangeKind classifies one supporting-file change.
type SupportChangeKind int

const (
	// SupportAdded reports a support file present only in the candidate.
	SupportAdded SupportChangeKind = iota
	// SupportRemoved reports a support file present only in the prior.
	SupportRemoved
	// SupportText reports changed bytes in a renderable text file.
	SupportText
	// SupportBinary reports changed bytes in a non-renderable file.
	SupportBinary
	// SupportModeOnly reports identical bytes with a changed executable mode.
	SupportModeOnly
	// SupportChanged reports changed content detected by digest without
	// retained bodies, as produced by a summary-only comparison.
	SupportChanged
)

func (k SupportChangeKind) String() string {
	switch k {
	case SupportAdded:
		return "added"
	case SupportRemoved:
		return "removed"
	case SupportText:
		return "text"
	case SupportBinary:
		return "binary"
	case SupportModeOnly:
		return "executable-mode"
	case SupportChanged:
		return "changed"
	default:
		return "unknown"
	}
}

// SupportChange is one changed supporting file with both sides' data. Hunks
// are rendered on demand from Old/New so a summary stays free of diff work.
type SupportChange struct {
	Path string
	Kind SupportChangeKind
	Old  *SupportFile
	New  *SupportFile
}

// ResourceChange is one classified change in the effective selected set.
// Prior and Candidate carry the sides' definitions for rendering; Candidate
// is nil for removals and Prior is nil for additions.
type ResourceChange struct {
	Kind              ChangeKind
	Identity          Identity // candidate identity; prior identity for removals
	OldRevision       string
	NewRevision       string
	Prior             *Side
	Candidate         *Side
	PriorDoc          string // normalized prior definition, when Prior != nil
	CandidateDoc      string // normalized candidate definition, when Candidate != nil
	DefinitionChanged bool
	Support           []SupportChange
}

// Compare classifies every change between two effective selected-resource
// sets. Matched occurrences with identical reviewed content and identical
// revision identity produce no record. An occurrence that moves between
// sources (a changed collision winner) is reported once with ChangeSource,
// but only when the move is unambiguous: exactly one prior and one candidate
// occurrence share the scope, kind, and installed name.
func Compare(prior, candidate []Side) ([]ResourceChange, error) {
	priorBySide := indexSides(prior)
	candidateBySide := indexSides(candidate)

	var changes []ResourceChange
	var removals, additions []Side
	for key, cand := range candidateBySide {
		old, matched := priorBySide[key]
		if !matched {
			additions = append(additions, cand)
			continue
		}
		change, err := compareMatched(old, cand)
		if err != nil {
			return nil, err
		}
		if change != nil {
			changes = append(changes, *change)
		}
	}
	for key, old := range priorBySide {
		if _, kept := candidateBySide[key]; !kept {
			removals = append(removals, old)
		}
	}

	paired := map[string]bool{}
	pairedChanges, err := pairSourceChanges(removals, additions, paired)
	if err != nil {
		return nil, err
	}
	changes = append(changes, pairedChanges...)
	for _, s := range removals {
		if paired[s.Identity.matchKey()] {
			continue
		}
		change, err := singleSideChange(s, false)
		if err != nil {
			return nil, err
		}
		changes = append(changes, *change)
	}
	for _, s := range additions {
		if paired[s.Identity.matchKey()] {
			continue
		}
		change, err := singleSideChange(s, true)
		if err != nil {
			return nil, err
		}
		changes = append(changes, *change)
	}
	sort.SliceStable(changes, func(i, j int) bool {
		return changes[i].Identity.sortKey() < changes[j].Identity.sortKey()
	})
	return changes, nil
}

func indexSides(sides []Side) map[string]Side {
	index := make(map[string]Side, len(sides))
	for _, s := range sides {
		index[s.Identity.matchKey()] = s
	}
	return index
}

func compareMatched(old, cand Side) (*ResourceChange, error) {
	if old.Resource == nil || cand.Resource == nil {
		return nil, fmt.Errorf("comparison side for %s %q has no resolved definition", cand.Identity.Kind, cand.Identity.Name)
	}
	_, oldDoc, err := Definition(old.Resource)
	if err != nil {
		return nil, err
	}
	_, candDoc, err := Definition(cand.Resource)
	if err != nil {
		return nil, err
	}
	definitionChanged := oldDoc != candDoc
	supportChanges := compareSupport(old.Support, cand.Support)
	if !definitionChanged && len(supportChanges) == 0 && old.Revision == cand.Revision {
		return nil, nil
	}
	change := &ResourceChange{
		Kind:              ChangeModified,
		Identity:          cand.Identity,
		OldRevision:       old.Revision,
		NewRevision:       cand.Revision,
		Prior:             &old,
		Candidate:         &cand,
		PriorDoc:          oldDoc,
		CandidateDoc:      candDoc,
		DefinitionChanged: definitionChanged,
		Support:           supportChanges,
	}
	if !definitionChanged && len(supportChanges) == 0 {
		change.Kind = ChangeRevisionOnly
	}
	return change, nil
}

func compareSupport(oldSupport, newSupport map[string]SupportFile) []SupportChange {
	if len(oldSupport) == 0 && len(newSupport) == 0 {
		return nil
	}
	paths := make(map[string]bool, len(oldSupport)+len(newSupport))
	for path := range oldSupport {
		paths[path] = true
	}
	for path := range newSupport {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var changes []SupportChange
	for _, path := range ordered {
		oldFile, hadOld := oldSupport[path]
		newFile, hadNew := newSupport[path]
		switch {
		case hadOld && !hadNew:
			old := oldFile
			changes = append(changes, SupportChange{Path: path, Kind: SupportRemoved, Old: &old})
		case !hadOld && hadNew:
			updated := newFile
			changes = append(changes, SupportChange{Path: path, Kind: SupportAdded, New: &updated})
		case supportContentEqual(oldFile, newFile):
			if oldFile.Executable != newFile.Executable {
				oldCopy, newCopy := oldFile, newFile
				changes = append(changes, SupportChange{Path: path, Kind: SupportModeOnly, Old: &oldCopy, New: &newCopy})
			}
		default:
			oldCopy, newCopy := oldFile, newFile
			kind := classifySupportChange(oldFile, newFile)
			changes = append(changes, SupportChange{Path: path, Kind: kind, Old: &oldCopy, New: &newCopy})
		}
	}
	return changes
}

// supportContentEqual reports whether both sides carry the same file content.
// Retained bodies compare by bytes; digest-only sides compare by digest.
func supportContentEqual(oldFile, newFile SupportFile) bool {
	if oldFile.Data != nil && newFile.Data != nil {
		return string(oldFile.Data) == string(newFile.Data)
	}
	if oldFile.Digest != "" && newFile.Digest != "" {
		return oldFile.Digest == newFile.Digest
	}
	return false
}

// classifySupportChange labels a changed supporting file. Digest-only sides
// (summary comparisons that did not retain bodies) report SupportChanged;
// retained bodies classify as renderable text or binary.
func classifySupportChange(oldFile, newFile SupportFile) SupportChangeKind {
	if oldFile.Data == nil || newFile.Data == nil {
		return SupportChanged
	}
	if RenderableText(oldFile.Data) && RenderableText(newFile.Data) {
		return SupportText
	}
	return SupportBinary
}

// pairSourceChanges pairs one unambiguous prior-removal with one
// candidate-addition sharing scope, kind, and installed name into a single
// ChangeSource record, marking both consumed sides in paired. Complementary
// occurrences (distinct scopes, paths, or refs) never pair and stay
// independent change records.
func pairSourceChanges(removals, additions []Side, paired map[string]bool) ([]ResourceChange, error) {
	removedByOccurrence := groupSides(removals)
	addedByOccurrence := groupSides(additions)
	var changes []ResourceChange
	for occurrence, removed := range removedByOccurrence {
		added := addedByOccurrence[occurrence]
		if len(removed) != 1 || len(added) != 1 {
			continue
		}
		old, cand := removed[0], added[0]
		if old.Resource == nil || cand.Resource == nil {
			return nil, fmt.Errorf("comparison side for %s %q has no resolved definition", cand.Identity.Kind, cand.Identity.Name)
		}
		_, oldDoc, err := Definition(old.Resource)
		if err != nil {
			return nil, err
		}
		_, candDoc, err := Definition(cand.Resource)
		if err != nil {
			return nil, err
		}
		paired[old.Identity.matchKey()] = true
		paired[cand.Identity.matchKey()] = true
		changes = append(changes, ResourceChange{
			Kind:              ChangeSource,
			Identity:          cand.Identity,
			OldRevision:       old.Revision,
			NewRevision:       cand.Revision,
			Prior:             &old,
			Candidate:         &cand,
			PriorDoc:          oldDoc,
			CandidateDoc:      candDoc,
			DefinitionChanged: oldDoc != candDoc,
			Support:           compareSupport(old.Support, cand.Support),
		})
	}
	return changes, nil
}

func groupSides(sides []Side) map[string][]Side {
	grouped := make(map[string][]Side, len(sides))
	for _, s := range sides {
		key := s.Identity.occurrenceKey()
		grouped[key] = append(grouped[key], s)
	}
	return grouped
}

func singleSideChange(s Side, added bool) (*ResourceChange, error) {
	if s.Resource == nil {
		return nil, fmt.Errorf("comparison side for %s %q has no resolved definition", s.Identity.Kind, s.Identity.Name)
	}
	_, doc, err := Definition(s.Resource)
	if err != nil {
		return nil, err
	}
	side := s
	change := &ResourceChange{Identity: s.Identity}
	change.Support = supportInventory(s.Support, added)
	if added {
		change.Kind = ChangeAdded
		change.NewRevision = s.Revision
		change.Candidate = &side
		change.CandidateDoc = doc
	} else {
		change.Kind = ChangeRemoved
		change.OldRevision = s.Revision
		change.Prior = &side
		change.PriorDoc = doc
	}
	return change, nil
}

// supportInventory reports every supporting file of a single-sided occurrence
// so the renderer can show the full added or removed set.
func supportInventory(support map[string]SupportFile, added bool) []SupportChange {
	paths := make([]string, 0, len(support))
	for path := range support {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	kind := SupportRemoved
	if added {
		kind = SupportAdded
	}
	changes := make([]SupportChange, 0, len(paths))
	for _, path := range paths {
		file := support[path]
		if added {
			changes = append(changes, SupportChange{Path: path, Kind: kind, New: &file})
		} else {
			changes = append(changes, SupportChange{Path: path, Kind: kind, Old: &file})
		}
	}
	return changes
}
