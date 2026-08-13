// Package devinprobe is the local-only, isolated runtime probe suite for the
// Devin CLI compatibility contract.
//
// It implements the probe half of work package D12 of
// docs/plans/2026-08-03-devin-first-class-harness.md and the "Local probe suite
// requirements" section of docs/devin-cli-harness-spec-3000.3.md:
//
//	"The implementation's local-only probe suite must use isolated temporary
//	HOME and repository roots. It must not inspect or mutate real user
//	configuration."
//
// # Running
//
//	make devin-probe
//
// That is the whole contract: one command. Without the opt-in environment
// variable the suite skips, so `go test ./...` and CI never depend on a
// third-party binary being installed, logged in, or on the network.
//
// # Isolation
//
// Isolation is structural, not conventional. Every probe goes through
// [newSandbox], which:
//
//  1. Allocates a fresh t.TempDir() and builds home/, home/.config/, and repo/
//     inside it. Nothing outside that tree is ever named by a probe.
//  2. Runs `devin` with an ALLOWLISTED environment ([sandbox.env]) rather than
//     os.Environ(). HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_DATA_HOME and
//     XDG_CACHE_HOME are all redirected into the sandbox. Only PATH and a
//     handful of inert locale/TMPDIR variables are inherited, so an ambient
//     DEVIN_* variable in the developer's shell cannot leak into a probe and a
//     probe cannot fall back to the real ~/.config/devin.
//  3. Asserts before every exec, via [sandbox.assertIsolated], that the
//     sandbox HOME differs from the real user home and that the real Devin
//     config directory is not inside the sandbox. A probe that would touch the
//     developer's configuration fails instead of running.
//  4. Runs the binary read-only. Every command in this suite is a documented
//     non-mutating command (`--version`, `skills paths|list|show`,
//     `rules paths|list`, `mcp list|get`). `devin mcp add/remove/enable/
//     disable`, `devin auth`, and any prompting invocation are deliberately
//     absent.
//
// The repository under test is never used as a probe workspace: probes `git
// init` a throwaway repo inside the sandbox, because Devin finds its project
// root by walking up for `.git`.
//
// # Release gating
//
// The plan makes one probe family release-blocking:
//
//	"Skills use directory symlinks and flat subagents use file symlinks. Both
//	shapes are release requirements: 3000.3 probes must pass for project and
//	user scopes before the target ships."
//
// Symlink probes therefore call [releaseBlocker] instead of t.Errorf. Their
// failure message is prefixed RELEASE BLOCKER so it cannot be mistaken for an
// ordinary test failure, and they never t.Skip on an assertion they were able
// to evaluate.
//
// The skill-directory half of that family runs here. The flat-subagent half
// could not: it needs a subagent enumeration surface that 3000.3.27 exposes
// only from inside a running session. It was therefore discharged by a manual
// authenticated run recorded in [ManualObservations], and
// [TestNoUnrunProbeIsReleaseBlocking] fails if a release-blocking gap is ever
// reintroduced into [UnrunProbes].
//
// # Probes that cannot run here
//
// Some probes the evidence snapshot asks for have no read-only surface in
// 3000.3.27 (there is no `devin hooks list`), so neither success nor failure
// is observable from a non-interactive command. Those are NOT silently omitted
// and NOT guessed: they are enumerated in [UnrunProbes] with the exact reason
// and the manual procedure, and [TestUnrunProbesAreReported] prints them on
// every run so a passing suite can never be read as full coverage.
//
// # Probes discharged by a manual authenticated run
//
// A second class of probe is unrunnable *here* but not unrunnable in
// principle: it needs an authenticated account (`devin models list`) or a
// surface that exists only inside a live session (subagent profile
// enumeration). This suite must never authenticate or reach the network, so
// those were run by hand on 2026-08-05 against the same build and recorded in
// [ManualObservations] with the version, host, method, negative controls, and
// remaining caveats.
//
// Three tests keep that arrangement honest, because prose alone would rot:
// [TestManualObservationsAreDocumented] keeps each record complete,
// [TestManualAndUnrunProbesAreDisjoint] stops a probe being both "settled" and
// "unrun", and [TestRetiredProbesKeepTheirManualEvidence] fails if a retired
// probe's evidence is deleted — retiring a probe is only defensible while the
// observation that retired it is still in the tree.
//
// A [ManualObservation] whose Caveat is non-empty did NOT fully discharge its
// probe, so the residue stays machine-readable in [UnrunProbes] rather than
// living only in that prose field.
package devinprobe
