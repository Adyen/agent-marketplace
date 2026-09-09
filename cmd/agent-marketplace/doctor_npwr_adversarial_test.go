package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// Adversarial review of bead agent-marketplace-npwr (B4), command level.
//
// The materialize half of this review found that a confirmed orphan removal must
// never delete a hook element the user was never shown, even when two hook
// elements are similar. With the total hook/v2 identity only the element whose
// canonical value matches the retained evidence can be nominated; any distinct
// hand-written element is unmanaged and is preserved. This file proves that
// invariant is reachable through the SHIPPED command — a human at a
// `doctor --repair` prompt answering y to a single, evidence-backed candidate —
// and pins the two off-TTY / EOF decline paths for the orphan loop.

const doctorTwinHandMarker = "USER-WROTE-THIS"

// prepareDoctorHookIdentityTwins is the command-level fixture: a project
// hooks.v1.json holding two similar hook elements, with retained evidence for
// the managed one only and no ownership record.
//
// With the total hook/v2 identity, the two elements have different identities,
// so only the managed element is an evidence-backed orphan candidate. The
// hand-written element is FIRST, which is the layout AGENTS.md documents
// ("handwritten hooks precede generated ones").
func prepareDoctorHookIdentityTwins(t *testing.T, repoRoot string) (hooksPath string, selection []resolver.ResolvedArtifact) {
	t.Helper()
	selection = []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{Name: "beta",
			Resource: ir.NewHook("beta", "PostToolUse", "echo beta", "command", nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	outDir := filepath.Join(repoRoot, ".devin")
	hooksPath = filepath.Join(outDir, harness.DevinProjectHooksFile)
	body := "{\n  \"PostToolUse\": [\n" +
		"    {\"type\":\"command\",\"command\":\"echo beta\",\"prompt\":\"" + doctorTwinHandMarker + "\"},\n" +
		"    {\"command\":\"echo beta\",\"type\":\"command\"}\n  ]\n}\n"
	if err := os.WriteFile(hooksPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatalf("drop the ownership record: %v", err)
	}
	return hooksPath, selection
}

// TestAdversarialDoctorRepairMustNotDeleteAnUnshownHookElement is the shipped-CLI
// proof of this review's headline finding.
//
// The user is offered exactly ONE orphan candidate, answers y once, and must not
// see any other element disappear. The hand-written element was never nominated,
// never printed, and never described by any evidence, so it must survive.
func scenarioAdversarialDoctorRepairMustNotDeleteAnUnshownHookElement(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	hooksPath, _ := prepareDoctorHookIdentityTwins(t, repoRoot)

	var out bytes.Buffer
	// Nothing is selected any more, so the managed element is undesired and its
	// retained backup manifest makes it an evidence-backed orphan candidate.
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(nil),
	}); err != nil {
		t.Fatalf("doctor --repair: %v\n%s", err, out.String())
	}
	if got := strings.Count(out.String(), "orphan candidate"); got == 0 {
		t.Fatalf("the fixture offered no orphan candidate:\n%s", out.String())
	}
	if strings.Count(out.String(), "[y/N]") != 1 {
		t.Fatalf("the fixture must prompt exactly once, so one answer maps to one entry:\n%s", out.String())
	}

	after, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), doctorTwinHandMarker) {
		t.Fatalf("one y at one prompt DELETED A HOOK ELEMENT THE USER WAS NEVER SHOWN.\n"+
			"The prompt described the evidence-backed managed element; the element removed is the "+
			"hand-written element, which was never nominated, never printed, and never described. "+
			"With a total hook identity it should have a different identity and be unmanaged, so it "+
			"must survive.\n"+
			"--- doctor output ---\n%s\n--- aggregate after ---\n%s", out.String(), after)
	}
}

// TestAdversarialDoctorRepairOrphanLoopDeclinesOnEOFAndOffTTY closes the two
// gaps the conflict loop already had covered but the orphan loop did not: an EOF
// arriving mid-prompt must DECLINE rather than error or delete, and off a
// terminal (including under --yes) the orphan must be reported and left alone.
//
// Both are confirmation invariants, which the review brief ranks with --yes: the
// only route to a deletion is an explicit interactive y.
func scenarioAdversarialDoctorRepairOrphanLoopDeclinesOnEOFAndOffTTY(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    string
		isTTY bool
		yes   bool
	}{
		{name: "EOF at the prompt on a TTY", in: "", isTTY: true},
		{name: "empty line on a TTY", in: "\n", isTTY: true},
		{name: "an unrelated answer on a TTY", in: "yes please\n", isTTY: true},
		{name: "--yes off a TTY", in: "y\ny\ny\n", isTTY: false, yes: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			hooksPath, _ := prepareDoctorHookIdentityTwins(t, repoRoot)
			before, err := os.ReadFile(hooksPath)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := runDoctor(doctorOptions{
				out:     &out,
				in:      strings.NewReader(tc.in),
				isTTY:   tc.isTTY,
				yes:     tc.yes,
				repair:  true,
				probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: staticResolve(nil),
			}); err != nil {
				t.Fatalf("a declined repair must exit 0: %v\n%s", err, out.String())
			}
			after, err := os.ReadFile(hooksPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("a repair with no explicit interactive y deleted an entry:\n"+
					"--- want ---\n%s\n--- got ---\n%s\n--- doctor output ---\n%s", before, after, out.String())
			}
		})
	}
}
