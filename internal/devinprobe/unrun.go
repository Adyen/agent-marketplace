package devinprobe

// UnrunProbe is a probe the evidence snapshot asks for that this suite cannot
// execute against Devin 3000.3.27, recorded explicitly so a green run is never
// mistaken for complete coverage.
type UnrunProbe struct {
	// ID is the stable identifier used in the certification document.
	ID string
	// Verifies is what a successful run would establish.
	Verifies string
	// Blocker is why it cannot run here.
	Blocker string
	// Manual is the procedure a human with the right access can follow.
	Manual string
	// ReleaseBlocking reports whether the plan's acceptance criteria make this
	// probe a release gate.
	ReleaseBlocking bool
}

// UnrunProbes is the list of probes that STILL remain unrun.
//
// Every entry was established empirically against devin 3000.3.27 (0becb483) on
// macOS while D12 was certified, not assumed: `devin --help` exposes no agents
// or hooks subcommand, and a malformed hooks.v1.json / mcp_config.json /
// config.json is silently ignored rather than reported, so "no error" is not
// evidence that a file was loaded.
//
// The list is deliberately not empty and must not be read as a to-do list that
// happens to be short: a fully green probe suite still is NOT complete coverage
// of the Devin target, because everything named here is unobserved rather than
// observed-working. What THIS list covers is hook loading, hook-comment
// acceptance, user-scope hook collection, MCP `${env:VAR}` interpolation, and
// the subagent-frontmatter residue left by the 2026-08-05 manual run (which
// model serves a spawned subagent, whether `max-nesting`'s VALUE bounds depth,
// and whether frontmatter `name:` overrides the path-derived identifier). All of
// them need an authenticated interactive session that the probe suite must never
// open.
//
// This list is NOT exhaustive of everything still unsettled about the Devin
// target, and must not be read as if it were. Further genuinely open items are
// tracked only in prose, in the "Still unsettled" section of
// docs/devin-verification-2026-08-04.md: `PermissionRequest` matcher
// acceptance, the default hook `type` when the key is absent, `timeout` units
// and zero-semantics, and the remote MCP transport vocabulary (which is not
// hook-related at all and was found unobservable on the read-only surface,
// because `devin mcp list`/`get` never echo the transport field). Nothing here
// covers those; whoever adds a probe for one of them should move it onto this
// list.
//
// The subagent-DISCOVERY, subagent-SYMLINK, and model-alias gaps are no longer
// open: they were retired by manual authenticated observation on 2026-08-05 and
// their evidence is recorded in ManualObservations (see manual.go). What those
// observations still do NOT establish is carried by their Caveat and by the
// subagent-frontmatter entry below, so the residue is machine-readable rather
// than prose-only. Nothing on this list is release-gating any more.
var UnrunProbes = []UnrunProbe{
	{
		ID:       "hook-loading-per-event",
		Verifies: "that command and prompt hooks load from .devin/hooks.v1.json for every documented event (PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, Stop, SessionStart, SessionEnd, PreCompact, PostCompact, PostCompaction), and that matcher/timeout are honoured as documented",
		Blocker:  "Devin 3000.3.27 exposes no `devin hooks` command, and a deliberately malformed .devin/hooks.v1.json produces no error from any read-only command, so the file's acceptance cannot be observed non-interactively.",
		Manual:   "In an isolated HOME with a logged-in account, install a PostToolUse command hook with matcher \"\" that appends its stdin payload to a file in the sandbox, run one prompt that uses a tool, and inspect the captured payload; repeat per event.",
	},
	{
		ID:       "hook-comment-acceptance",
		Verifies: "that Devin still loads .devin/hooks.v1.json and ~/.config/devin/config.json after Agent Marketplace's syntax-preserving patch leaves handwritten // and /* */ comments in place",
		Blocker:  "Same absence of a hook observation surface. Comment acceptance IS verified for mcp_config.json (see the runtime probe suite), but that result cannot be transferred to the hook files without observing them.",
		Manual:   "As above, with a comment inserted next to the handwritten hook entry; confirm the hook still fires.",
	},
	{
		ID:       "user-hooks-in-config-json",
		Verifies: "that hooks patched into the `hooks` member of ~/.config/devin/config.json are collected, and that unrelated settings in that file keep working",
		Blocker:  "No hook observation surface, and a malformed config.json is silently ignored, so neither success nor failure is observable read-only.",
		Manual:   "As for hook-loading-per-event, with the hook installed at user scope.",
	},
	{
		ID:       "mcp-env-interpolation",
		Verifies: "that ${env:VAR} interpolation in mcp_config.json env values, headers, and oauthClientSecret is expanded at server launch",
		Blocker:  "Expansion happens when a server is launched, which requires an authenticated session and would execute a third-party process; `devin mcp get` redacts env and header values rather than showing the expanded result.",
		Manual:   "With a logged-in account, configure a stdio MCP server whose command echoes an interpolated env value, start a session, and inspect the server's received environment.",
	},
	{
		// This is the residue of the 2026-08-05 manual run: three frontmatter
		// claims the ManualObservations Caveat says were NOT established, kept
		// here so the gap is machine-readable and not prose-only. It is NOT
		// release-gating: the release gate was symlink following at both scopes,
		// which was observed.
		ID:       "subagent-frontmatter-model-and-nesting-honored",
		Verifies: "which model actually serves a spawned custom subagent (i.e. that the compiled `model:` value is honoured at inference time, not merely accepted at parse time); that the VALUE of `max-nesting` bounds nesting depth rather than merely enabling nesting; and that a frontmatter `name:` overrides the identifier derived from the file path, as /usr/local/share/devin/docs/subagents.mdx:224-259 documents",
		Blocker:  "Devin 3000.3.27 does not label a running subagent's model anywhere in the CLI, so the serving model cannot be read off any surface. The 2026-08-05 manual authenticated run (see ManualObservations) observed only that the nesting tools were PRESENT — consistent with any budget >= 1 — because it never exercised a nesting-depth limit, and it used a profile whose file basename equalled its frontmatter `name`, so nothing distinguished a frontmatter-derived identifier from a path-derived one.",
		Manual:   "In an isolated but authenticated HOME, install three profiles and spawn each from a `devin -p` session. (1) Model: pin a distinctive `model:` (e.g. `haiku` while the parent session runs on `opus`) and ask the spawned subagent to report its own model, cross-checking against any billing/telemetry surface that names it, since self-report alone is not evidence. (2) Nesting depth: install a profile with `max-nesting: 1`, ask it to nest twice (spawn a subagent that itself spawns one), and require the SECOND level to be refused; repeat with `max-nesting: 2` and require it to succeed, so the value and not merely the presence of nesting is what changed. (3) Name override: install a profile at `agents/file-basename.md` whose frontmatter says `name: different-identifier`, then enumerate profiles and require `different-identifier` — not `file-basename` — to be the spawnable identifier.",
	},
}
