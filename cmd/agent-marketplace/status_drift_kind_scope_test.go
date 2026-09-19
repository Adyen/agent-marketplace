package main

import (
	"errors"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
)

// TestSuppressIncompletePinRemovalsScopesByKindAndNamespace pins the
// kind-aware suppression contract: a drift-incomplete pin suppresses only
// removal candidates in the output namespaces its kind materializes into that
// carry its installed name. Same-named removals in an unrelated kind's
// namespace or root are real drift and must stay visible.
func TestSuppressIncompletePinRemovalsScopesByKindAndNamespace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	pins := []marketplace.DriftIncompletePin{
		{Err: errors.New("cache wiped"), Kind: "skill", Name: "review"},
	}
	report := &materialize.Report{
		PerRoot: []materialize.TargetReport{
			// Harness-root report: paths carry their namespace prefix.
			{OutDir: "/repo/.claude", Removed: []string{"skills/review", "agents/review.md", "commands/review.md"}},
			// Namespace-directory reports: the OutDir itself is the
			// namespace, so bare names and same-named files in another
			// kind's directory must be told apart.
			{OutDir: "/repo/.factory/skills", Removed: []string{"review"}},
			{OutDir: "/repo/.factory/agents", Removed: []string{"review.md"}},
		},
	}

	filtered, suppressed := suppressIncompletePinRemovals(report, pins)
	if suppressed != 2 {
		t.Fatalf("suppressed = %d, want 2 (the skill-namespace candidates)", suppressed)
	}
	wantRemoved := [][]string{
		{"agents/review.md", "commands/review.md"},
		{},
		{"review.md"},
	}
	for i, tr := range filtered.PerRoot {
		if !slices.Equal(tr.Removed, wantRemoved[i]) {
			t.Fatalf("root %d removed = %v, want %v", i, tr.Removed, wantRemoved[i])
		}
	}
	// The input report is never mutated.
	if len(report.PerRoot[0].Removed) != 3 {
		t.Fatalf("input report mutated: removed = %v", report.PerRoot[0].Removed)
	}
}

// TestSuppressIncompletePinRemovalsKeepsUnattributableNamespaceCandidates
// covers the conservative edge: when neither the path nor the report
// directory identifies an output namespace, attribution cannot be disproven,
// so kinds that own a namespace still suppress while kinds without one (mcp
// servers in shared aggregates, virtual collections) suppress nothing.
func TestSuppressIncompletePinRemovalsKeepsUnattributableNamespaceCandidates(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	pins := []marketplace.DriftIncompletePin{
		{Err: errors.New("cache wiped"), Kind: "skill", Name: "review"},
		{Err: errors.New("cache wiped"), Kind: "collection", Name: "bundle"},
	}
	report := &materialize.Report{
		PerRoot: []materialize.TargetReport{
			{OutDir: "/repo/.codex", Removed: []string{"review", "bundle", "skills/bundle"}},
		},
	}

	filtered, suppressed := suppressIncompletePinRemovals(report, pins)
	// "review" has no determinable namespace and the skill kind owns one, so
	// it stays suppressed; the collection pin owns no output namespace, so
	// neither "bundle" nor the skills-namespace removal is suppressed.
	if suppressed != 1 {
		t.Fatalf("suppressed = %d, want 1", suppressed)
	}
	if !slices.Equal(filtered.PerRoot[0].Removed, []string{"bundle", "skills/bundle"}) {
		t.Fatalf("removed = %v, want the collection-unattributable candidates visible", filtered.PerRoot[0].Removed)
	}
}
