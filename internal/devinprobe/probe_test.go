package devinprobe

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/devincli"
)

// TestProbeVersionMeetsFloor is probe 1 of the evidence snapshot's required
// list: "Print and assert `devin --version` is at least 3000.3."
func TestProbeVersionMeetsFloor(t *testing.T) {
	probe := requireDevin(t)
	if !probe.Version.AtLeastFloor() {
		t.Fatalf("installed Devin %s is below the %s floor", probe.Version, devincli.FloorString)
	}
	t.Logf("PROBE PASS version: devin %s at %s satisfies the %s floor", probe.Version, probe.Path, devincli.FloorString)
}

// TestProbeCanonicalSkillPaths is probe 2: "Verify canonical project and user
// skill discovery." It asserts both that `devin skills paths` still advertises
// the two canonical roots the target writes to, and that a skill placed at each
// root is actually loaded.
func TestProbeCanonicalSkillPaths(t *testing.T) {
	probe := requireDevin(t)
	s := newSandbox(t, probe)

	s.writeSkill(s.ProjectRoot, "probe-project-skill", "description: project scope probe\n", "project body")
	s.writeSkill(s.UserRoot, "probe-user-skill", "description: user scope probe\n", "user body")

	paths := s.mustRun("skills", "paths")
	for _, want := range []string{
		filepath.Join(s.UserRoot, "skills"),
		filepath.Join(".devin", "skills"),
	} {
		if !strings.Contains(paths, want) {
			t.Errorf("`devin skills paths` no longer advertises %q:\n%s", want, paths)
		}
	}
	// The sandbox HOME must appear in the output; if it does not, the binary
	// ignored our HOME override and the whole suite is meaningless.
	if !strings.Contains(paths, s.Home) {
		t.Fatalf("REFUSING TO TRUST THIS RUN: `devin skills paths` did not report the sandbox home %s, so HOME was not honoured:\n%s", s.Home, paths)
	}

	list := s.mustRun("skills", "list")
	for _, name := range []string{"probe-project-skill", "probe-user-skill"} {
		if !strings.Contains(list, name) {
			t.Errorf("skill %q at its canonical root was not discovered:\n%s", name, list)
		}
		show := s.mustRun("skills", "show", name)
		if !strings.Contains(show, name) {
			t.Errorf("`devin skills show %s` did not resolve the skill:\n%s", name, show)
		}
	}
}

// TestProbeAllowedToolVocabulary is probe 4: "Recompute the complete
// `allowed-tools` acceptance set."
//
// It is the runtime re-verification the mapping table's provenance comment and
// the evidence snapshot both call for: the set was pinned against 3000.2.17 and
// the snapshot says "re-verification required on 3000.3.27 before
// implementation completion".
//
// The probe is written as an acceptance test over a candidate superset rather
// than a spot check of the five expected names, because the risk it guards is
// Devin GAINING a tool the compiler would then wrongly reject, not just losing
// one.
func TestProbeAllowedToolVocabulary(t *testing.T) {
	probe := requireDevin(t)
	s := newSandbox(t, probe)

	// The canonical mapping targets. If any of these stops being accepted, the
	// compiler is emitting a restriction Devin silently drops, which BROADENS
	// privilege — the exact failure the plan forbids.
	want := []string{"read", "edit", "grep", "glob", "exec"}
	// Plausible alternative spellings and tools from neighbouring harnesses. If
	// any of these starts being accepted, the mapping table is incomplete and
	// resources that could be represented are being failed instead.
	candidates := append([]string{}, want...)
	candidates = append(candidates,
		"write", "ls", "bash", "shell", "run", "search", "fetch", "web", "websearch",
		"todo", "task", "think", "plan", "mcp", "notebook", "multiedit", "apply_patch",
	)

	var accepted []string
	for _, tool := range candidates {
		name := "probe-tool-" + strings.ReplaceAll(tool, "_", "-")
		s.writeSkill(s.ProjectRoot, name, "description: tool probe\nallowed-tools:\n  - "+tool+"\n", "body")
		out := s.mustRun("skills", "show", name)
		if toolLineHas(out, tool) {
			accepted = append(accepted, tool)
		}
	}
	slices.Sort(accepted)
	sorted := slices.Clone(want)
	slices.Sort(sorted)
	if !slices.Equal(accepted, sorted) {
		t.Fatalf("the Devin allowed-tools vocabulary changed.\naccepted: %v\nmapping table targets: %v\nUpdate pkg/harness-compilation/compile/mapping/tools.go and docs/devin-cli-harness-spec-3000.3.md together.",
			accepted, sorted)
	}
	t.Logf("PROBE PASS tools: accepted allowed-tools set is exactly %v; every other candidate is silently dropped", accepted)
}

// toolLineHas reports whether `devin skills show` listed the tool as allowed.
// An unaccepted tool yields an empty "Allowed tools:" line, which is precisely
// the silent filtering the evidence snapshot warns about under "Known
// documentation/runtime risks".
func toolLineHas(show, tool string) bool {
	for _, line := range strings.Split(show, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), "allowed tools:") {
			continue
		}
		value := strings.TrimSpace(trimmed[len("allowed tools:"):])
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == tool {
				return true
			}
		}
	}
	return false
}

// TestProbeCommandProvenanceExtension is probe 5: "Verify command-origin
// extension acceptance."
//
// The evidence snapshot grades this Inferred: "Devin's frontmatter parser
// preserves acceptance of this unknown namespaced key. Add a fixture and
// runtime probe before declaring command-origin round trips complete."
func TestProbeCommandProvenanceExtension(t *testing.T) {
	probe := requireDevin(t)
	s := newSandbox(t, probe)

	s.writeSkill(s.ProjectRoot, "probe-command", "description: converted command probe\nx-agent-marketplace-kind: command\ntriggers:\n  - user\n", "command body")
	s.writeSkill(s.ProjectRoot, "probe-plain-skill", "description: plain skill probe\n", "skill body")

	show := s.mustRun("skills", "show", "probe-command")
	if !strings.Contains(show, "probe-command") {
		t.Fatalf("a skill carrying x-agent-marketplace-kind was rejected; command provenance is not representable:\n%s", show)
	}
	// The plan requires "A converted command skill is emitted with
	// triggers: [user], no model trigger". Devin must honour that, or a
	// converted command would still be model-invocable.
	triggers := extractField(show, "Triggers:")
	if triggers != "user" {
		t.Errorf("converted command triggers = %q, want exactly \"user\" (no model trigger)\n%s", triggers, show)
	}
	plain := s.mustRun("skills", "show", "probe-plain-skill")
	if got := extractField(plain, "Triggers:"); !strings.Contains(got, "model") {
		t.Errorf("an ordinary Devin skill lost its model trigger (got %q); the extension must not change default skills\n%s", got, plain)
	}
}

// extractField pulls the value of a `Label: value` line out of devin output.
func extractField(out, label string) string {
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, label) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, label))
		}
	}
	return ""
}

// TestProbeMcpAggregate is probe 7 ("Verify stdio and HTTP MCP loading,
// headers, disabled state") and probe 8 ("Verify comments/order-preserving
// aggregate output remains accepted") for the mcp_config.json aggregate.
//
// The evidence snapshot grades all of this Inferred: "Comment handling,
// unknown-field handling, disabled-state round trip, and exact interpolation
// behavior require isolated 3000.3.27 probes."
//
// The fixture is deliberately shaped like Agent Marketplace output AFTER a
// syntax-preserving patch: a handwritten leading comment, a comment associated
// with an unmanaged key, and unknown fields, all of which the jsonc patcher
// preserves.
func TestProbeMcpAggregate(t *testing.T) {
	probe := requireDevin(t)
	s := newSandbox(t, probe)

	s.writeFile(filepath.Join(s.ProjectRoot, "mcp_config.json"), `{
  // handwritten header comment; the jsonc patcher must preserve it
  "mcpServers": {
    // comment associated with an unmanaged key
    "probe-stdio": {
      "command": "echo",
      "args": ["--flag"],
      "env": {"PROBE_SETTING": "probe-value"},
      "disabled": false
    },
    "probe-http": {
      "url": "https://example.invalid/mcp",
      "transport": "http",
      "headers": {"X-Probe-Client": "probe"},
      "oauthClientId": "probe-client-id",
      "oauthResource": "https://example.invalid",
      "disabled": true
    },
    "probe-unknown-fields": {
      "command": "echo",
      "someUnknownDevinKey": 1
    }
  }
}
`)

	list := s.mustRun("mcp", "list")
	for _, name := range []string{"probe-stdio", "probe-http", "probe-unknown-fields"} {
		if !strings.Contains(list, name) {
			t.Fatalf("MCP server %q was not loaded; a comment-bearing mcp_config.json is not accepted:\n%s", name, list)
		}
	}
	if !strings.Contains(strings.ToLower(list), "disabled") {
		t.Errorf("`devin mcp list` did not report the disabled state:\n%s", list)
	}

	stdio := s.mustRun("mcp", "get", "probe-stdio")
	if !strings.Contains(stdio, "echo") {
		t.Errorf("stdio command was not loaded:\n%s", stdio)
	}
	http := s.mustRun("mcp", "get", "probe-http")
	if !strings.Contains(http, "https://example.invalid/mcp") {
		t.Errorf("HTTP url was not loaded:\n%s", http)
	}
	if !strings.Contains(strings.ToLower(http), "disabled") {
		t.Errorf("the disabled HTTP server did not report its disabled state:\n%s", http)
	}
	if !strings.Contains(http, "X-Probe-Client") {
		t.Errorf("HTTP headers were not loaded:\n%s", http)
	}
	// Devin redacts header and env VALUES itself. The probe records that,
	// because it is the reason `devin mcp get` is safe to run in a suite whose
	// output is pasted into reports.
	if strings.Contains(http, "probe") && strings.Contains(http, "X-Probe-Client: probe") {
		t.Logf("NOTE: devin printed a header value verbatim; keep probe fixtures free of real secrets")
	}
	unknown := s.mustRun("mcp", "get", "probe-unknown-fields")
	if !strings.Contains(unknown, "probe-unknown-fields") {
		t.Errorf("a server carrying an unknown field was rejected:\n%s", unknown)
	}
}

// TestProbeRulesDiscoveryUnderDevinRoots is probe 10.
//
// It was written to verify that "rules remain absent from canonical `.devin`
// paths", which the plan treats as the runtime justification for the
// compiler's stable unsupported-rule diagnostic. **That assumption is false on
// 3000.3.27.** `devin rules paths` advertises only `.windsurf/rules` and
// `.cursor/rules`, but a rule file written to `.devin/rules/<name>.md` IS
// discovered and listed.
//
// The probe therefore records what Devin actually does rather than asserting
// the falsified expectation. No emitted artifact is wrong today — rules are an
// explicit non-goal of the promoted plan, so the compiler emitting nothing is
// still correct behaviour — but the diagnostic's stated RATIONALE ("Devin has
// no rule primitive") is not what the runtime shows. Bead
// agent-marketplace-5exj.22 owns restating it as a scope decision, or adding
// rule support.
//
// The probe fails only if the advertised paths and the discovery behaviour
// agree again in the other direction, i.e. if Devin starts advertising a
// `.devin` rules path — at which point rule support is unambiguously expected
// and the scope decision has to be revisited.
func TestProbeRulesDiscoveryUnderDevinRoots(t *testing.T) {
	probe := requireDevin(t)
	s := newSandbox(t, probe)

	paths := s.mustRun("rules", "paths")
	advertised := strings.Contains(paths, filepath.Join(".devin", "rules")) || strings.Contains(paths, filepath.Join(s.UserRoot, "rules"))

	s.writeFile(filepath.Join(s.ProjectRoot, "rules", "probe-rule.md"), "# probe rule\n")
	discovered := strings.Contains(s.mustRun("rules", "list"), "probe-rule")

	switch {
	case advertised:
		t.Fatalf("Devin now ADVERTISES a canonical rules path under a .devin root, so rules are a first-class Devin primitive and the target must gain rule support instead of emitting rule.devin.unsupported (bead agent-marketplace-5exj.22):\n%s", paths)
	case discovered:
		t.Logf("PROBE OBSERVED rules: .devin/rules is not advertised by `rules paths` but a rule placed there IS discovered; rule.devin.unsupported remains a scope decision, not a capability limit (bead agent-marketplace-5exj.22)")
	default:
		t.Logf("PROBE PASS rules: .devin/rules is neither advertised nor discovered")
	}
}

// TestProbeSymlinkProjectSkillDirectory is the project half of probe 9 and is a
// RELEASE GATE:
//
//	"Skills use directory symlinks and flat subagents use file symlinks. Both
//	shapes are release requirements: 3000.3 probes must pass for project and
//	user scopes before the target ships. There is no real-file fallback in this
//	increment."
func TestProbeSymlinkProjectSkillDirectory(t *testing.T) {
	probe := requireDevin(t)
	s := newSandbox(t, probe)
	assertSkillDirectorySymlinkIsFollowed(t, s, s.ProjectRoot, "probe-project-symlink", "project")
}

// TestProbeSymlinkUserSkillDirectory is the user half of probe 9 and is a
// RELEASE GATE. See TestProbeSymlinkProjectSkillDirectory.
func TestProbeSymlinkUserSkillDirectory(t *testing.T) {
	probe := requireDevin(t)
	s := newSandbox(t, probe)
	assertSkillDirectorySymlinkIsFollowed(t, s, s.UserRoot, "probe-user-symlink", "user")
}

// assertSkillDirectorySymlinkIsFollowed materializes a skill the way
// internal/materialize does — a directory symlink from the canonical root into
// a content store standing in for the registry cache — and requires Devin to
// follow it.
func assertSkillDirectorySymlinkIsFollowed(t *testing.T, s *sandbox, root, name, scope string) {
	t.Helper()
	store := filepath.Join(s.Root, "cache", name)
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("create content store: %v", err)
	}
	s.writeFile(filepath.Join(store, "SKILL.md"),
		"---\nname: "+name+"\ndescription: "+scope+" scope directory symlink probe\n---\nbody\n")

	link := filepath.Join(root, "skills", name)
	s.symlink(store, link)
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("probe fixture is wrong: %s is not a symlink (%v)", link, err)
	}

	list, err := s.run("skills", "list")
	if err != nil {
		releaseBlocker(t, "`devin skills list` failed with a %s-scope skill directory symlink present: %v\n%s", scope, err, list)
	}
	if !strings.Contains(list, name) {
		releaseBlocker(t, "Devin %s does not follow a %s-scope skill DIRECTORY symlink at %s. The plan allows no real-file fallback, so the Devin target must not ship.\n%s",
			s.version, scope, link, list)
	}
	show, err := s.run("skills", "show", name)
	if err != nil || !strings.Contains(show, name) {
		releaseBlocker(t, "Devin listed but could not resolve the %s-scope symlinked skill %s: %v\n%s", scope, name, err, show)
	}
	t.Logf("PROBE PASS symlink-skill-%s: Devin followed the directory symlink %s -> %s", scope, link, store)
}

// TestUnrunProbesAreReported prints every probe that could not run, so a green
// suite is never mistaken for complete coverage. It is not gated on the binary:
// the gap list is a fact about Devin 3000.3.27, not about this machine.
func TestUnrunProbesAreReported(t *testing.T) {
	if len(UnrunProbes) == 0 {
		return
	}
	for _, p := range UnrunProbes {
		if p.ID == "" || p.Verifies == "" || p.Blocker == "" || p.Manual == "" {
			t.Errorf("UnrunProbe %q is incompletely documented; every entry must say what it verifies, why it cannot run, and how to run it manually", p.ID)
		}
		gate := ""
		if p.ReleaseBlocking {
			gate = " [RELEASE-GATING per acceptance criterion 15]"
		}
		t.Logf("UNRUN PROBE %s%s\n  verifies: %s\n  blocked by: %s\n  manual: %s", p.ID, gate, p.Verifies, p.Blocker, p.Manual)
	}
}

// TestNoUnrunProbeIsReleaseBlocking is a REGRESSION GUARD, not a proof that
// acceptance criterion 15 is satisfied.
//
// Be precise about what it can and cannot do. `ReleaseBlocking` is currently set
// on no entry at all, so the loop below has nothing to reject and passes
// vacuously; it establishes nothing positive about criterion 15. What actually
// satisfies that criterion is the recorded manual observation of both symlink
// probes against an authenticated Devin 3000.3.27 (see ManualObservations), plus
// the live skill-symlink probes above. This test's only job is to make the gate
// impossible to reopen quietly: if someone re-adds a release-blocking unrun
// probe — or discovers a new gap and marks it correctly — the build must go red
// instead of the flag sitting unnoticed in a slice literal.
//
// Like TestUnrunProbesAreReported it does not run the binary: the gate is a fact
// about the recorded lists, not about this machine.
func TestNoUnrunProbeIsReleaseBlocking(t *testing.T) {
	for _, p := range UnrunProbes {
		if p.ReleaseBlocking {
			// Deliberately not releaseBlocker: that helper is labelled for the
			// symlink probes and is fatal, and here every offending entry should
			// be named in one run rather than only the first.
			t.Errorf("RELEASE BLOCKER (unrun probe): UnrunProbe %q is still marked ReleaseBlocking, so acceptance criterion 15 is NOT satisfied and the Devin target must not ship.\n  verifies: %s\n  blocked by: %s\n  manual: %s",
				p.ID, p.Verifies, p.Blocker, p.Manual)
		}
	}
	t.Logf("PROBE PASS release-gate: none of the %d remaining unrun probes is release-blocking; %d probes (including both formerly release-gating symlink probes) were retired by recorded manual observation", len(UnrunProbes), len(ManualObservations))
}

// retiredByManualObservation is the list of probe IDs that were removed from
// UnrunProbes on the strength of the 2026-08-05 manual authenticated run.
//
// It is written out literally, and not derived from ManualObservations, on
// purpose: deriving it would make the assertion below circular. Acceptance
// criterion 15 needs the evidence for the two formerly release-gating symlink
// probes to STAY in the tree, and until this list existed, deleting four of the
// five ManualObservations entries left the suite green because nothing asserted
// they were there. Now it goes red.
//
// A probe may only be added here once its evidence is recorded in manual.go.
var retiredByManualObservation = []string{
	"symlink-subagent-project",
	"symlink-subagent-user",
	"subagent-discovery-project",
	"subagent-discovery-user",
	"model-alias-list",
}

// TestRetiredProbesKeepTheirManualEvidence asserts that every probe retired from
// UnrunProbes still has its evidence recorded in ManualObservations. Removing an
// entry from manual.go must be a test failure, because the retirement of a
// release-gating probe rests on nothing else.
//
// Uniqueness within each list, and disjointness between the two, are asserted by
// TestManualAndUnrunProbesAreDisjoint; this test adds only the PRESENCE direction
// those cannot cover — an empty list is trivially disjoint from everything.
func TestRetiredProbesKeepTheirManualEvidence(t *testing.T) {
	recorded := make(map[string]bool, len(ManualObservations))
	for _, o := range ManualObservations {
		recorded[o.ID] = true
		// The reverse direction: an observation nobody claims retired anything is
		// either a missing entry above or evidence that has lost its subject.
		if !slices.Contains(retiredByManualObservation, o.ID) {
			t.Errorf("ManualObservation %q is not listed in retiredByManualObservation; record which probe it retired so the two lists cannot drift", o.ID)
		}
	}
	for _, id := range retiredByManualObservation {
		if !recorded[id] {
			t.Errorf("probe %q was retired from UnrunProbes on the strength of manual evidence, but ManualObservations has no entry for it; the evidence must stay in the tree (acceptance criterion 15)", id)
		}
	}
}

// manualEvidenceVersion extracts the Devin version an observation's Evidence
// cites, e.g. "devin 3000.3.27 (0becb483) at ..." -> 3000.3.27.
var manualEvidenceVersion = regexp.MustCompile(`\bdevin (\d+)\.(\d+)\.(\d+)\b`)

// TestManualObservationsAreDocumented prints every manual observation, so a
// suite run reports the recorded evidence exactly the way
// TestUnrunProbesAreReported reports the remaining gaps.
//
// It also enforces that the evidence is usable by someone who was not in the
// room: an observation with no method, no controls, or no provenance is an
// assertion, not evidence, and must not be allowed to retire a probe. Caveat is
// the one optional field — empty means the retired probe's claim is fully
// covered.
//
// This test never executes the devin binary and needs neither authentication nor
// network: it asserts over the recorded list only.
func TestManualObservationsAreDocumented(t *testing.T) {
	if len(ManualObservations) == 0 {
		t.Fatal("ManualObservations is empty, but probes were retired from UnrunProbes on the strength of manual evidence; the evidence must stay in the tree")
	}
	for _, o := range ManualObservations {
		for _, f := range []struct{ name, value string }{
			{"ID", o.ID},
			{"Verifies", o.Verifies},
			{"Observed", o.Observed},
			{"Method", o.Method},
			{"Evidence", o.Evidence},
			{"Controls", o.Controls},
		} {
			if strings.TrimSpace(f.value) == "" {
				t.Errorf("ManualObservation %q has an empty %s; a manual observation must say what was seen, how, with what controls, and on which build before it can retire a probe", o.ID, f.name)
			}
		}

		m := manualEvidenceVersion.FindStringSubmatch(o.Evidence)
		if m == nil {
			t.Errorf("ManualObservation %q does not name a devin version in its Evidence (%q); the capture's build must be recorded", o.ID, o.Evidence)
			continue
		}
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		version := devincli.Version{Major: major, Minor: minor, Patch: patch}
		if !version.AtLeastFloor() {
			t.Errorf("ManualObservation %q was captured on devin %s, below the %s floor, so it says nothing about a supported Devin", o.ID, version, devincli.FloorString)
		}

		caveat := o.Caveat
		if caveat == "" {
			caveat = "(none recorded)"
		}
		t.Logf("MANUAL OBSERVATION %s\n  verifies: %s\n  observed: %s\n  method: %s\n  evidence: %s\n  controls: %s\n  caveat: %s",
			o.ID, o.Verifies, o.Observed, o.Method, o.Evidence, o.Controls, caveat)
	}
}

// TestManualAndUnrunProbesAreDisjoint is the guard that stops the two lists
// drifting: a probe is either settled by recorded manual evidence or still
// unrun, never both, and no ID may appear twice within either list.
//
// Without this, retiring a probe by adding a ManualObservation while forgetting
// to delete the UnrunProbe would leave the tree claiming both at once.
func TestManualAndUnrunProbesAreDisjoint(t *testing.T) {
	unrun := make(map[string]bool, len(UnrunProbes))
	for _, p := range UnrunProbes {
		if unrun[p.ID] {
			t.Errorf("UnrunProbes lists %q more than once; probe IDs must be unique", p.ID)
		}
		unrun[p.ID] = true
	}

	seen := make(map[string]bool, len(ManualObservations))
	for _, o := range ManualObservations {
		if seen[o.ID] {
			t.Errorf("ManualObservations lists %q more than once; observation IDs must be unique", o.ID)
		}
		seen[o.ID] = true
		if unrun[o.ID] {
			t.Errorf("probe %q is recorded as manually observed AND still unrun; retiring a probe means deleting its UnrunProbes entry, not shadowing it", o.ID)
		}
	}
}
