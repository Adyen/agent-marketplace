// Devin aggregate REPAIR primitives (work package D11 of
// docs/plans/2026-08-03-devin-first-class-harness.md).
//
// This file implements the plan's recovery surface:
//
//	"`doctor --repair` previews exact adoption matches and evidence-backed
//	orphan candidates; on a TTY it then prompts for each authorized action"
//	"off-TTY, `doctor --repair` is preview-only"
//	"`doctor --repair --yes` adopts exact current desired matches without prompting"
//	"orphan-removal candidates require non-heuristic retained journal or
//	safety-backup evidence linking the entry to prior managed output, plus
//	explicit per-entry confirmation and a fresh backup"
//
// It also implements the THIRD repair class, which the plan's two classes could
// not describe (bead agent-marketplace-0w2g): an existing unmanaged entry whose
// KEY matches a desired entry but whose VALUE differs. A collision needs only
// key equality, adoption needs value equality, and orphan nomination needs
// retained evidence that only a completed mutation produces — so such an entry
// was reported by every sync and repairable by nothing. See [ConflictCandidate]
// and [RepairFile.ReplaceConflict]; it destroys user-visible content, so it
// inherits orphan removal's contract, not adoption's.
//
// Nothing here decides ownership, exactness, evidence, or difference. Those
// questions are answered exclusively by internal/ownership's
// [ownership.ExactAdoptionCandidates], [ownership.Store.OrphanCandidates],
// [ownership.ValueConflictCandidates], and [ownership.Proof], whose result types
// cannot be constructed outside that package. This file only decides which
// candidates the user authorized and turns an authorized candidate into a
// journaled mutation.
//
// Two structural properties matter more than any check written here:
//
//  1. [DevinRepairPreview] never writes. It opens stores, reads aggregates and
//     ownership records, and returns candidates. There is no mutation path
//     reachable from it, which is what makes "off-TTY, doctor --repair is
//     preview-only" a property of the code rather than of a flag test.
//  2. [RepairFile.RemoveOrphan] and [RepairFile.ReplaceConflict] take a
//     `confirmed` argument they pass straight into
//     ownership.Mutation.Confirmed, and ownership.Apply refuses either
//     unconfirmed with ownership.ErrConfirmationRequired. A caller cannot
//     delete or overwrite a user's entry without ASSERTING per-entry consent;
//     that the assertion corresponds to a real interactive answer is doctor's
//     half of the contract, and the two halves fail independently.
//
// No message, hint, or log attribute in this file carries a hook payload, a
// matcher, a timeout value, an MCP env value, a header, an OAuth secret, or
// aggregate file content. Only aggregate paths (install root plus
// output-relative path, so two aggregates under one root are distinguishable),
// non-secret entry identities (an MCP server object key or a hook event name),
// resource names, digests, and generation numbers are reported.
package materialize

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// DevinRepairPreview computes exact adoption matches and evidence-backed orphan
// candidates for every Devin aggregate file the current configuration touches.
//
// It is READ-ONLY by construction: it never calls Store.Apply, Store.Recover,
// or Store.BackupOnly, and it never writes a file. That is the whole of
// "off-TTY, `doctor --repair` is preview-only" — the off-TTY path simply stops
// here.
func DevinRepairPreview(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact) (*RepairPreview, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	slog.Info("devin repair preview start", "repo_root", repoRoot)
	preview := &RepairPreview{}

	aggregates, keys, diags := devinDesiredAggregates(repoRoot, resolved)
	preview.Diags = append(preview.Diags, diags...)

	roots, ok := harness.MakeInstallRoots()[hcc.TargetDevin]
	if !ok {
		return preview, nil
	}

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		outDir := roots.For(key.machineScope, repoRoot)
		info, ok := harness.TargetInfoMap[hcc.TargetDevin]
		if !ok {
			continue
		}
		rels := append([]string(nil), info.AggregateFilesForScope(key.machineScope)...)
		sort.Strings(rels)

		desiredByRel := make(map[string][]ownedJSONEntry, len(rels))
		if work := aggregates[key]; work != nil {
			for _, entry := range work.entries {
				desiredByRel[entry.rel] = append(desiredByRel[entry.rel], entry)
			}
		}

		var store *ownership.Store
		for _, rel := range rels {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			abs := filepath.Join(outDir, rel)
			if _, err := os.Lstat(abs); err != nil {
				// No aggregate on disk means there is nothing to adopt and
				// nothing to remove. An absent file is not a repair candidate.
				continue
			}
			if store == nil {
				// The observer is installed on the PREVIEW's store because that
				// is the store the three mutators later act through: a
				// RepairFile carries it. The preview itself takes no lock, so
				// installing one here is also how a test asserts that — a
				// preview that produced a single lock event would be a
				// side-effecting read.
				opened, err := ownership.Open(outDir, ownership.WithLockObserver(observeRepairLock))
				if err != nil {
					preview.Diags = append(preview.Diags, ownedAggregateDiag(diag.CodeMaterializeDevinAggregateWrite, key, "",
						fmt.Sprintf("open ownership store for %s: %v", outDir, err)))
					break
				}
				store = opened
			}
			file, fileDiags := previewOwnedJSONAggregateFile(store, key, outDir, devinOwnedJSONFile(rel), devinOwnedJSONOptions(), false, desiredByRel[rel])
			preview.Diags = append(preview.Diags, fileDiags...)
			if file != nil {
				preview.Files = append(preview.Files, file)
			}
		}
	}

	slog.Info("devin repair preview completed",
		"repo_root", repoRoot, "files", len(preview.Files), "diagnostics", len(preview.Diags))
	return preview, nil
}

// devinDesiredAggregates compiles the resolved selection and returns the DESIRED
// Devin aggregate entries per output key, without touching disk.
//
// It reuses the same buildWorkItems/compileWorkItems phases sync uses, filtered
// to the Devin target, so the desired set a repair compares against is the same
// desired set sync would write. Deriving it a second, independent way would
// make "exact match" mean two different things in two commands.
func devinDesiredAggregates(repoRoot string, resolved []resolver.ResolvedArtifact) (map[outputKey]*ownedJSONAggregateWork, []outputKey, []diag.Diagnostic) {
	compiled, keys := compileRepairTarget(repoRoot, resolved, hcc.TargetDevin)
	return compiled.devinAggregates, keys, compiled.allDiags
}

// compileRepairTarget reuses sync's compile phases for one repair provider.
// This keeps exact-match classification tied to the bytes sync would write.
func compileRepairTarget(repoRoot string, resolved []resolver.ResolvedArtifact, target hcc.Target) (compilePhaseResult, []outputKey) {
	// The fourth return is the whole-run diagnostic list (nil-resource warnings
	// for every target). It is dropped rather than reported: those warnings
	// belong to sync, and repeating another target's warning in a target-local
	// repair preview would be noise the user cannot act on here.
	work, allOutputs, blocked, _ := buildWorkItems(resolved, repoRoot)

	targetWork := work[:0:0]
	for _, item := range work {
		if item.target == target {
			targetWork = append(targetWork, item)
		}
	}
	targetOutputs := make(map[outputKey]bool)
	for key := range allOutputs {
		if key.target == target {
			targetOutputs[key] = true
		}
	}
	targetBlocked := make(blockedOutputs)
	for key, components := range blocked {
		if key.target != target {
			continue
		}
		targetBlocked.mark(key)
		for component := range components {
			targetBlocked.mark(key, component)
		}
	}

	compiled := compileWorkItems(targetWork, nil, targetOutputs, targetBlocked, repoRoot)

	keys := make([]outputKey, 0, len(targetOutputs))
	for key := range targetOutputs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].machineScope != keys[j].machineScope {
			return !keys[i].machineScope
		}
		return string(keys[i].target) < string(keys[j].target)
	})

	return compiled, keys
}
