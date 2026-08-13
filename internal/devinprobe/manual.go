package devinprobe

// ManualObservation records one probe that the automated suite cannot run, but
// that a human HAS run against a real, authenticated Devin CLI. It is the
// counterpart of UnrunProbe: an UnrunProbe says "nobody has looked at this", a
// ManualObservation says "somebody looked, here is exactly what they saw, here
// is how, and here is what it still does not prove".
//
// Entries are evidence, not conclusions. They are written once, from a recorded
// session, and are never re-derived by running anything: the tests over this
// list assert only that each entry is fully documented, never that the
// observation can be reproduced here. Reproducing any of these requires an
// authenticated account and an interactive session, both of which the probe
// suite is forbidden to use.
type ManualObservation struct {
	// ID matches the ID of the UnrunProbe this observation retired, so the two
	// lists stay one-to-one and cannot silently drift.
	ID string
	// Verifies is what the observation establishes, in the same terms the
	// retired UnrunProbe used.
	Verifies string
	// Observed is what actually happened, stated in observable terms: what was
	// asked, and what came back.
	Observed string
	// Method is how it was run, including HOME isolation and how authentication
	// was carried into the isolated environment.
	Method string
	// Evidence is the provenance of the capture: binary version and build, host,
	// date, and source tree.
	Evidence string
	// Controls are the negative and differential controls that make the
	// observation non-vacuous — the reasons it cannot be explained by a cache, a
	// filename, or the model guessing.
	Controls string
	// Caveat is what the observation still does NOT establish. Empty means the
	// retired probe's claim is fully covered.
	Caveat string
}

// manualEvidence2026Aug05 is the shared provenance of the 2026-08-05 capture.
// Every observation below was recorded in that one session, so the string is
// stated once rather than paraphrased per entry.
const manualEvidence2026Aug05 = "devin 3000.3.27 (0becb483) at /usr/local/bin/devin; " +
	"host Darwin 25.6.0 arm64 (macOS, Apple silicon); " +
	"recorded 2026-08-05T10:00:20Z; " +
	"tree branch devin-support at commit 2d060e4, agent-marketplace built via `make build` as v2.3.2-46-g2d060e4"

// manualMethodSubagent is the setup shared by the four subagent observations.
const manualMethodSubagent = "Fresh `mktemp -d` sandbox with HOME overridden to $SB/home and " +
	"AGENT_MARKETPLACE_ORG_CONFIG pointed at a nonexistent file to null out the embedded org layer. " +
	"Authentication survived the isolated HOME because Devin stores credentials at " +
	"$HOME/.local/share/devin/credentials.toml, which was copied into the sandbox HOME. " +
	"`devin auth status` reported \"Logged in (via Devin)\". LIMIT OF THE ISOLATION, stated exactly: it " +
	"rested on the HOME override ALONE. Unlike the automated sandbox in sandbox_test.go, which also exports " +
	"XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_DATA_HOME and XDG_CACHE_HOME, this run overrode only HOME and " +
	"AGENT_MARKETPLACE_ORG_CONFIG, did not export any XDG_* variable, and did not record that none was " +
	"already set in that shell; no filesystem instrumentation (fs_usage, dtruss) was used. So the claim " +
	"is that no real path under ~/.config/devin, ~/.cache or ~/.agents was ADDRESSED BY CONSTRUCTION of " +
	"the HOME override — it is not an observed negative, and an XDG_* variable inherited from the shell " +
	"could in principle have redirected a lookup back outside the sandbox. Devin's non-interactive " +
	"--print mode refuses an untrusted workspace, so --respect-workspace-trust false was passed " +
	"(documented flag, /usr/local/share/devin/docs/reference/commands.mdx:32-36); per those docs the flag " +
	"affects trust only, not resource discovery, but that is a documentation claim and was NOT observed, " +
	"and every observation below was therefore taken in trust-bypassed --print mode rather than in the " +
	"default interactive configuration. Fixture: a plain local-directory registry with two Claude-layout subagents " +
	"(model: sonnet, tools: [Read, Grep], max-nesting: 2); ~/.agents/config.yaml (machine scope) selected " +
	"probe-agent-user with harnesses: [devin] and <repo>/.agents/config.yaml (repo scope) selected " +
	"probe-agent-project. One `agent-marketplace sync -v` reported \"Installed 2, Removed 0, Malformed 0, " +
	"Errors 0\" with zero warnings. Enumeration surface: a Devin session's own system prompt lists the " +
	"profiles available to its run_subagent tool, so asking the agent to enumerate them reads what Devin " +
	"actually loaded."

// manualControlsSubagent is the control set shared by the four subagent
// observations. Each control removes one alternative explanation.
const manualControlsSubagent = "CONTROL A (scope separation): the same prompt run from a directory OUTSIDE " +
	"the sandbox repo returned only probe-agent-user, subagent_explore and subagent_general — the project " +
	"profile disappeared, so the two scopes are independently confirmed rather than conflated. " +
	"CONTROL B (body read through the link): asked for each custom profile's description, Devin returned " +
	"\"Project-scope Devin subagent symlink probe\" and \"User-scope Devin subagent symlink probe\", text " +
	"that exists only inside the symlink targets' frontmatter, so Devin dereferenced the links and parsed " +
	"the compiled frontmatter instead of listing a filename. " +
	"CONTROL C-project (causality, project scope): renaming the project profile's cache target directory " +
	"aside so the symlink dangled (`test -e` false, the link itself untouched) made probe-agent-project " +
	"vanish from enumeration, leaving only probe-agent-user and the two builtins; renaming the target " +
	"back restored it. " +
	"CONTROL C-user (causality, user scope — the same control, run for the other scope so the two are " +
	"symmetric): renaming the USER-scope profile's cache target directory " +
	"(.../compiled/devin/5282fb21938cbdc5/subagent/probe-agent-user) aside so " +
	"~/.config/devin/agents/probe-agent-user.md dangled (`test -e` false, the symlink itself untouched) " +
	"made probe-agent-user vanish too: enumeration run from a directory OUTSIDE the sandbox repo returned " +
	"ONLY subagent_explore and subagent_general. After renaming the target back, a final enumeration from " +
	"inside the repo again returned all four (probe-agent-project, probe-agent-user, subagent_explore, " +
	"subagent_general). Enumeration is therefore driven by the symlink resolving at BOTH scopes, not by a " +
	"cache, the filename, or the model guessing. " +
	"CONTROL E (allowed-tools enforcement, verified on the filesystem rather than self-reported): a file " +
	"target.txt containing \"hello-original\" was created in the sandbox repo root, and the spawned " +
	"probe-agent-project (compiled allowed-tools: [read, grep]) was asked to overwrite it with " +
	"\"CHANGED\" using any edit/write tool and then to run `echo ran-exec`. It reported possessing only " +
	"grep, read, read_subagent, run_subagent — no write/edit tool and no exec/shell tool — and that " +
	"neither action could therefore be performed, and target.txt was INDEPENDENTLY CONFIRMED on disk " +
	"afterwards to still contain \"hello-original\". The restriction is thus enforced in effect, not " +
	"merely described by the agent. The same exchange also showed the bound is on the profile's OWN " +
	"toolset: the subagent observed that it could in principle delegate to the write-capable " +
	"subagent_general, nesting was not restricted, and max-nesting: 2 permitted it."

// manualCaveatSubagent is the honest limit shared by all four subagent
// observations. Three frontmatter claims are NOT established by the capture —
// the model that actually served the subagent, the VALUE of max-nesting, and
// frontmatter `name:` overriding the path-derived identifier — and the
// enforcement of allowed-tools is bounded to the profile's own toolset. The
// residue is tracked machine-readably as the UnrunProbe
// subagent-frontmatter-model-and-nesting-honored (see unrun.go).
const manualCaveatSubagent = "Which model actually served the spawned subagent was NOT verified: " +
	"Devin does not label a running subagent's model, so the compiled `model: sonnet` frontmatter is " +
	"observed to be present and accepted, not observed to be honored at inference time. " +
	"`max-nesting`'s VALUE is likewise unverified: only the PRESENCE of the nesting tools " +
	"(run_subagent, read_subagent) in the spawned profile was observed, which is consistent with any " +
	"nesting budget >= 1, so `max-nesting: 2` is not observed to bound depth at 2 — no nesting-depth " +
	"limit was exercised. Frontmatter `name:` overriding the path-derived identifier is UNEXERCISED: the " +
	"file basename equalled the frontmatter `name`, so nothing in the capture distinguishes a " +
	"frontmatter-derived identifier from a path-derived one. And CONTROL E establishes that " +
	"`allowed-tools` bounds the profile's OWN toolset only: the restricted profile could still in " +
	"principle delegate to the write-capable builtin subagent_general, nesting was not restricted, and " +
	"nothing in the capture bounds what the profile can reach THROUGH nesting. This residue is tracked as " +
	"the UnrunProbe subagent-frontmatter-model-and-nesting-honored."

// ManualObservations is the recorded evidence that retired five formerly unrun
// probes — the two subagent-discovery probes, the two release-gating
// subagent-symlink probes, and model-alias-list — on 2026-08-05.
//
// The project and user halves are kept as separate entries so the IDs stay
// one-to-one with the probes they retired, even where the underlying capture is
// a single session covering both scopes.
//
// Devin's own docs accept TWO custom-subagent layouts under either canonical
// root (.devin/agents/ and ~/.config/devin/agents/): a flat file
// agents/<name>.md, or a directory agents/<name>/AGENT.md (with AGENTS.md,
// agent.md and agents.md also accepted as the file name). In both layouts a
// frontmatter `name:` overrides the path-derived identifier
// (/usr/local/share/devin/docs/subagents.mdx:224-259). What the compiler emits,
// and the only layout this run exercised, is the FLAT-FILE form: the directory
// form is exactly what
// pkg/harness-compilation/compile/devin/subagent_test.go's
// TestSubagentArtifactShapeSupportsFileSymlink forbids the compiler from
// emitting, because it would turn the release-gated file symlink into a
// directory symlink. The directory layout was not observed here. Those docs
// also mark custom subagents "experimental", so none of this is a stability
// guarantee.
var ManualObservations = []ManualObservation{
	{
		ID:       "subagent-discovery-project",
		Verifies: "that Devin loads a flat project subagent profile from .devin/agents/<name>.md, including the name/description/model/allowed-tools/max-nesting frontmatter the Devin compiler emits",
		Observed: "Run from inside the sandbox repo, `devin -p` asked to list every run_subagent profile returned exactly probe-agent-project, probe-agent-user, subagent_explore, subagent_general. Asked to actually spawn probe-agent-project in the foreground, it ran and returned its designated token PROBE-PROJECT-OK, which exists only in the compiled body — so the compiled system prompt reached the spawned subagent. The spawned profile reported its tools as exactly grep, read, read_subagent, run_subagent: no edit and no exec, i.e. exactly the compiled `allowed-tools: [read, grep]` plus the two nesting tools, and CONTROL E then confirmed the restriction on the filesystem (an overwrite it was asked to perform did not happen). What this establishes, exactly: the profile loads, is enumerated, is spawnable, its `description` reaches Devin, and its `allowed-tools` is enforced. It does NOT establish anything about the VALUE of `max-nesting` (only that nesting tools are present, which any budget >= 1 would produce) or about frontmatter `name:` overriding a path-derived identifier (the basename equalled the name) — see Caveat.",
		Method:   manualMethodSubagent + " The project profile was installed at <repo>/.devin/agents/probe-agent-project.md.",
		Evidence: manualEvidence2026Aug05,
		Controls: manualControlsSubagent,
		Caveat:   manualCaveatSubagent,
	},
	{
		ID:       "subagent-discovery-user",
		Verifies: "the same for ~/.config/devin/agents/<name>.md",
		Observed: "The user-scope profile probe-agent-user appeared in the same enumeration, and appeared ALSO when the prompt was run from outside the sandbox repo, where the project profile did not. Spawned in the foreground it ran and returned its designated token PROBE-USER-OK, which exists only in the compiled body. Causality was then established for this scope too (CONTROL C-user): with the user profile's cache target renamed aside, enumeration from outside the repo returned only subagent_explore and subagent_general, and restoring the target restored all four profiles. So the profile loads, is enumerated, is spawnable, and its `description` reaches Devin, on the same footing as the project half.",
		Method:   manualMethodSubagent + " The user profile was installed at $SB/home/.config/devin/agents/probe-agent-user.md, i.e. under the sandbox user root rather than the project root.",
		Evidence: manualEvidence2026Aug05,
		Controls: manualControlsSubagent,
		Caveat:   manualCaveatSubagent,
	},
	{
		ID:       "symlink-subagent-project",
		Verifies: "that Devin follows a FILE symlink at .devin/agents/<name>.md pointing into the Agent Marketplace cache",
		Observed: "`agent-marketplace sync -v` wrote <repo>/.devin/agents/probe-agent-project.md -> $SB/home/.cache/agent-marketplace/compiled/devin/ca85092e1ed6a7a4/subagent/probe-agent-project/probe-agent-project.md. The installed path is a FILE symlink (`test -L` true) whose target is a regular file inside the Devin compile cache, and the resolved body carried exactly name, description, model: sonnet, allowed-tools: [read, grep], max-nesting: 2. Devin enumerated the profile, returned its description (text present only in the symlink target), and spawned it successfully. Devin follows the file symlink on 3000.3.27; this was the release gate for project scope.",
		Method:   manualMethodSubagent + " The link was produced by a real `agent-marketplace sync`, not hand-built, so the observed shape is the shape the target actually ships.",
		Evidence: manualEvidence2026Aug05,
		Controls: manualControlsSubagent,
		Caveat:   manualCaveatSubagent,
	},
	{
		ID:       "symlink-subagent-user",
		Verifies: "the same for ~/.config/devin/agents/<name>.md",
		Observed: "The same sync wrote $SB/home/.config/devin/agents/probe-agent-user.md -> $SB/home/.cache/agent-marketplace/compiled/devin/5282fb21938cbdc5/subagent/probe-agent-user/probe-agent-user.md, also a FILE symlink to a regular file in the compile cache, with the same resolved frontmatter. Devin enumerated it from outside the repo too, returned its description, and spawned it, returning PROBE-USER-OK. Dangling that same target (CONTROL C-user) removed it from enumeration and restoring the target brought it back, so resolution of THIS link is what produced the profile. Devin follows the user-scope file symlink on 3000.3.27; this was the release gate for user scope.",
		Method:   manualMethodSubagent + " Same single sync; only the installation root differs.",
		Evidence: manualEvidence2026Aug05,
		Controls: manualControlsSubagent,
		Caveat:   manualCaveatSubagent,
	},
	{
		ID:       "model-alias-list",
		Verifies: "that the stable aliases the mapping table targets (sonnet, opus, haiku) are still present in `devin models list`, and which model family each currently resolves to",
		Observed: "`devin models list` succeeded and reported 28 model families. Stable aliases present: opus -> Claude Opus 5; sonnet and claude -> Claude Sonnet 5; haiku -> Claude Haiku 4.5; gemini -> Gemini 3.5 Flash; gpt -> GPT-5.6 Terra; codex -> GPT-5.3-Codex; swe -> SWE-1.7. A gemini-3.1-pro family EXISTS in the listing but carries NO alias. Generation drift, recorded rather than smoothed over: the alias SET is unchanged from the previous capture, but the families behind the aliases moved — swe now resolves to SWE-1.7, gpt to GPT-5.6 Terra, and Devin's sonnet/haiku to Claude Sonnet 5 / Claude Haiku 4.5 while the mapping table's other target cells in those same rows pin Claude Sonnet 4.5 / Claude Haiku 3.5. (Inferences drawn from this reading are deliberately NOT stated here; see Caveat and the provenance comments in pkg/harness-compilation/compile/mapping/models.go.)",
		Method:   "Run in the same isolated authenticated sandbox HOME as the subagent observations: HOME overridden to $SB/home with credentials.toml copied in — with the same limit stated there, namely that isolation rested on the HOME override alone, no XDG_* variable was overridden, and no filesystem instrumentation was used, so \"the real user configuration was not read\" is by construction of the override rather than an observed negative. `devin models list` requires an authenticated account (unauthenticated it exits with \"Not logged in. Run `devin auth login` to authenticate.\"), which is exactly why the probe suite cannot reproduce this: it must never authenticate or reach the network.",
		Evidence: manualEvidence2026Aug05,
		Controls: "Differential, not merely confirmatory: the listing was read for absences as well as presences. gemini-3.1-pro appears as a family with no alias while gemini does have one, so the Flash/Pro asymmetry encoded in the mapping table is read off the same output that confirms the aliases, rather than assumed.",
		Caveat: "Inference, and its untested premise. The reading supports the mapping table's sonnet/opus/haiku Devin cells (those aliases are present) and the Gemini Flash -> `gemini` cell. It does NOT establish the Pro-class conclusion on its own: nobody attempted `model: gemini-3.1-pro`, so \"the Pro-class family carries no alias\" does not establish \"there is no usable Pro-class identifier\" — a bare family name might well be accepted as a `model:` value. gemini-3-preview stays modelUnmappable because the plan forbids depending on an unverified identifier, not because non-usability was observed. " +
			"Also a dated capture, not a contract: the family list and the alias->family bindings are a point-in-time reading of a server-side catalogue. The aliases can be re-pointed at new families with no change to the local 3000.3.27 binary — this capture already shows that happening (swe -> SWE-1.7, gpt -> GPT-5.6 Terra, sonnet -> Claude Sonnet 5, haiku -> Claude Haiku 4.5) — so the mapping table's Devin cells need re-capture, not just a version check. Targeting aliases rather than versioned families is the deliberate choice that makes that drift tolerable; it also means a Devin alias may serve a newer family than the pinned IDs in the same table row.",
	},
}
