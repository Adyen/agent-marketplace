// Package devin implements the Ingester for canonical Devin CLI 3000.3 source
// layouts (work package D8 of docs/plans/2026-08-03-devin-first-class-harness.md).
//
// # What "canonical Devin input" means here
//
// Exactly two shapes, and nothing else:
//
//	workspace root   <root>/.devin/{skills,agents,hooks.v1.json,mcp_config.json,config.json}
//	direct user root <root>/{skills,agents,config.json,mcp_config.json}
//
// The workspace shape is the only one autodetection recognizes. The direct
// shape — the layout of `~/.config/devin`, and also the layout the Devin
// compiler materializes into a target root — "requires explicit `--from devin`
// or `source_harness: devin` because that shape is too ambiguous for
// autodetection" (plan, "Bidirectional conversion"): a bare `skills/` plus
// `agents/` at a root is equally a Claude plugin. Detect therefore reports only
// the workspace shape, while LoadBundle — which is only reached once a caller
// has NAMED devin — accepts both.
//
// # What this ingester deliberately does not do
//
//   - It does not walk ancestors. There is no upward search for `.git`, `.jj`,
//     or a parent `.devin`; the fs.FS it is handed IS the root. Devin's runtime
//     does walk upward, and emulating that would silently pull in
//     configuration from outside the directory the user pointed at.
//   - It does not walk descendants. A `.devin` inside a subdirectory is not a
//     candidate root, and neither is `.devin/.devin`.
//   - It does not merge roots. Project and user configuration are never
//     combined: "One conversion input represents one explicit source root;
//     project and user roots are never merged implicitly."
//   - It does not read compatibility layouts. `.agents`, `.claude`, `.cursor`,
//     and `.windsurf` are other source formats with their own ingesters.
//   - It does not read the project-local personal files
//     `config.local.json` / `mcp_config.local.json`, and it does not read the
//     pre-3000.3 `config.json#mcpServers` block. Both are reported rather than
//     silently skipped.
//
// # Credential hygiene
//
// No diagnostic message, hint, or slog attribute produced by this package
// carries a command string, a prompt body, a matcher value, a timeout value, an
// env value, a header value, an OAuth client id/secret/resource, a permission
// value, or a resource body. Only field names, the fixed Devin event and tool
// vocabularies, resource names, and source paths are ever reported.
// TestDevinIngestDiagnosticsNeverQuoteValues pins it.
package devin

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Canonical Devin 3000.3 path components, relative to a Devin root. They mirror
// the unexported constants in compile/devin/translator.go on purpose: this
// package is the exact inverse of that one, and the two path contracts are
// pinned against each other by TestDevinIngestMirrorsCompilerPaths.
const (
	// workspaceDir is the project marker directory. It is the ONLY directory
	// name this package ever prepends, and it is never searched for: it is
	// either present directly under the supplied root or the root is treated as
	// a direct root.
	workspaceDir = ".devin"

	skillsDir       = "skills"
	agentsDir       = "agents"
	skillFile       = "SKILL.md"
	subagentSuffix  = ".md"
	subagentDirFile = "AGENT.md"

	projectHooksFile   = "hooks.v1.json"
	configFile         = "config.json"
	mcpConfigFile      = "mcp_config.json"
	localConfigFile    = "config.local.json"
	localMcpConfigFile = "mcp_config.local.json"

	// hooksKey is the config.json member that carries user-scope hooks.
	hooksKey = "hooks"
	// legacyMcpServersKey is the pre-3000.3 config.json member. It is detected
	// so the loss can be reported, and never read.
	legacyMcpServersKey = "mcpServers"
)

func init() {
	ingest.Register(&devinIngester{})
}

// devinIngester is the registered Devin source-format ingester.
type devinIngester struct{}

// Source returns Devin as the ingester's source harness.
func (*devinIngester) Source() ingest.Harness { return ingest.HarnessDevin }

// Detect reports whether src is a canonical Devin WORKSPACE root.
//
// It deliberately returns false for a direct user root: the plan requires that
// shape to be selected explicitly, and Detect is what autodetection consults.
//
// Detection reads a fixed, ordered list of literal marker paths and never
// depends on the order fs.ReadDir returns entries in, so the answer is the same
// on every filesystem and every run.
func (*devinIngester) Detect(src fs.FS) bool { return hasWorkspaceMarkers(src) }

// DetectStrict implements ingest.StrictDetector.
//
// Devin has no permissive fallback form to filter out — Detect already
// recognizes exactly one shape — so strict detection is the same predicate.
// It is implemented anyway so sub-bundle scanning routes through the same
// answer instead of silently falling back to Detect.
func (*devinIngester) DetectStrict(src fs.FS) bool { return hasWorkspaceMarkers(src) }

// Detect reports whether src is a canonical Devin workspace root. It is the
// package-level form of the registered ingester's Detect, matching the sibling
// ingesters' exported helper shape.
func Detect(src fs.FS) bool { return (&devinIngester{}).Detect(src) }

// LoadBundle reads one explicit Devin root. See the package comment for the two
// accepted shapes and for everything this function refuses to do.
func LoadBundle(src fs.FS) (*ingest.Bundle, error) { return (&devinIngester{}).LoadBundle(src) }

// root is the resolved Devin root: the single path prefix every read in this
// package is relative to.
//
// It exists so the prefix is computed exactly once, at the top of LoadBundle,
// from a fixed marker list. Nothing downstream may re-resolve it, which is what
// makes "no ancestor or nested discovery" a structural property rather than a
// convention.
type root struct {
	// prefix is "" for a direct root and ".devin" for a workspace root.
	prefix string
	// workspace records which shape was selected, for logging and for the
	// user-root invariant tests.
	workspace bool
}

// join builds a path inside the resolved root.
func (r root) join(elements ...string) string {
	if r.prefix == "" {
		return path.Join(elements...)
	}
	return path.Join(append([]string{r.prefix}, elements...)...)
}

// String names the shape for structured logging. It is a fixed vocabulary term.
func (r root) String() string {
	if r.workspace {
		return "workspace"
	}
	return "direct"
}

// workspaceMarkerFiles and workspaceMarkerDirs are the canonical `.devin`
// resource/config markers autodetection recognizes.
//
// Plan: "Workspace autodetection recognizes canonical `.devin` resource/config
// markers." The lists are literal and ordered; membership is a pure function of
// which paths exist, never of directory iteration order.
//
// `config.local.json` and `mcp_config.local.json` are NOT markers: a root whose
// only Devin content is a personal override file has nothing this target may
// materialize into or read from.
var (
	workspaceMarkerFiles = []string{projectHooksFile, mcpConfigFile, configFile}
	workspaceMarkerDirs  = []string{skillsDir, agentsDir}
)

func hasWorkspaceMarkers(src fs.FS) bool {
	return hasMarkers(src, root{prefix: workspaceDir, workspace: true})
}

func hasDirectMarkers(src fs.FS) bool {
	return hasMarkers(src, root{})
}

func hasMarkers(src fs.FS, r root) bool {
	for _, name := range workspaceMarkerFiles {
		if fileExists(src, r.join(name)) {
			return true
		}
	}
	for _, name := range workspaceMarkerDirs {
		if entries, err := fs.ReadDir(src, r.join(name)); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

// LoadBundle reads every supported resource from one explicit Devin root.
func (*devinIngester) LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	r, ok := resolveRoot(src)
	if !ok {
		return nil, fmt.Errorf("ingest/devin: no recognizable Devin layout found: expected a %s/ workspace root or an explicitly selected direct root containing %s/, %s/, %s, or %s",
			workspaceDir, skillsDir, agentsDir, configFile, mcpConfigFile)
	}
	slog.Info("devin ingest start", "source", ingest.HarnessDevin, "root_form", r.String())

	// Devin has no bundle manifest in either shape, so the manifest is always
	// synthesized. Reported as info for the same reason the Gemini and Windsurf
	// ingesters report it: a consumer that expected registry metadata should
	// learn that none existed rather than see an empty name.
	diags := []diag.Diagnostic{{
		Code:     diag.CodeIngestDevinManifestSynthesized,
		Severity: diag.SeverityInfo,
		Resource: "bundle",
		Message:  "Devin roots carry no bundle manifest; manifest synthesized from the layout",
		Hint:     "Devin has no plugin manifest file; bundle metadata is synthesized during ingestion",
	}}
	diags = append(diags, localOverrideDiagnostics(src, r)...)

	var resources []ir.Resource

	skills, skillDiags, err := loadSkills(src, r)
	if err != nil {
		return nil, err
	}
	diags = append(diags, skillDiags...)
	resources = append(resources, skills...)

	subagents, subagentDiags, err := loadSubagents(src, r)
	if err != nil {
		return nil, err
	}
	diags = append(diags, subagentDiags...)
	resources = append(resources, subagents...)

	hooks, hookDiags := loadHooks(src, r)
	diags = append(diags, hookDiags...)
	resources = append(resources, hooks...)

	servers, mcpDiags := loadMcpServers(src, r)
	diags = append(diags, mcpDiags...)
	resources = append(resources, servers...)

	diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestDevinUnconsumedConfig, "devin "+r.String(), r.join(projectHooksFile), r.join(configFile), r.join(mcpConfigFile))...)
	slog.Info("devin ingest end", "source", ingest.HarnessDevin, "root_form", r.String(),
		"resources", len(resources), "diagnostics", len(diags))
	return &ingest.Bundle{Resources: resources, Diagnostics: diags}, nil
}

// resolveRoot picks the single root this run reads from.
//
// The workspace shape wins when both are present, which is the same precedence
// autodetection applies: a repository that has been materialized into `.devin`
// and also happens to carry a top-level `skills/` directory is a workspace, and
// reading both would be the implicit merge the plan forbids.
func resolveRoot(src fs.FS) (root, bool) {
	if hasWorkspaceMarkers(src) {
		return root{prefix: workspaceDir, workspace: true}, true
	}
	if hasDirectMarkers(src) {
		return root{}, true
	}
	return root{}, false
}

// localOverrideDiagnostics reports the project-local personal files this target
// never reads.
//
// Evidence: "Project-local personal files include `.devin/config.local.json`
// and `.devin/mcp_config.local.json`; Agent Marketplace does not materialize
// into these paths." Not materializing into them is a write rule; not READING
// them is the matching read rule, because a resource recovered from a personal
// override would be published into a shared bundle. Reporting it keeps the
// omission visible instead of making a converted bundle look complete.
func localOverrideDiagnostics(src fs.FS, r root) []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, name := range []string{localConfigFile, localMcpConfigFile} {
		file := r.join(name)
		if !fileExists(src, file) {
			continue
		}
		slog.Info("devin local override ignored", "source", ingest.HarnessDevin, "file", file)
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestDevinLocalOverrideIgnored,
			Severity: diag.SeverityInfo,
			Resource: "bundle",
			Message:  fmt.Sprintf("%q is a Devin project-local personal file and was not ingested", file),
			Hint:     "Agent Marketplace neither reads nor writes Devin .local.json files; move shared configuration into the non-local file",
			Source:   diag.SourceRef{File: file, Line: 0},
		})
	}
	return diags
}

// reverseTools maps a Devin allowed-tools list back to canonical tool IDs.
//
// Devin's vocabulary is CLOSED (read, edit, grep, glob, exec), so a name
// outside it is not "an unknown tool to pass through" — it is a restriction
// Devin itself silently ignores (evidence baseline, "Known documentation and
// runtime risks": "Unknown tool names may be silently filtered by Devin").
// Carrying it into canonical IR would turn an unenforced string into an
// enforced restriction on the next target, or, on the way back to Devin, fail
// with a compiler error that blames the wrong layer. Both are wrong in a way
// that changes what the agent may do, so the resource fails closed here.
//
// The offending value is never reported: allowed-tools entries carry
// Claude-style scoped permission strings such as `Bash(git push --force:*)`,
// i.e. command strings the plan keeps out of diagnostics and logs. The resource
// name and source location localize the failure instead.
func reverseTools(tools []string, resource, file string) ([]string, []diag.Diagnostic) {
	if tools == nil {
		return nil, nil
	}
	canonical := make([]string, 0, len(tools))
	unreversible := 0
	for _, tool := range tools {
		mapped, ok := mapping.ReverseTool(tool, mapping.TargetDevin)
		if !ok {
			unreversible++
			continue
		}
		canonical = append(canonical, mapped)
	}
	if unreversible == 0 {
		return canonical, nil
	}
	slog.Warn("devin allowed-tools entry unreversible", "source", ingest.HarnessDevin,
		"resource", resource, "unreversible_count", unreversible)
	return nil, []diag.Diagnostic{{
		Code:     diag.CodeIngestDevinToolUnreversible,
		Severity: diag.SeverityError,
		Resource: resource,
		Message:  "an allowed-tools entry is outside Devin's closed read/edit/grep/glob/exec vocabulary; the resource was not ingested",
		Hint:     "Devin silently ignores unknown tool names, so the restriction is not enforced; fix the allowed-tools list rather than letting an unenforced restriction become canonical IR",
		Source:   diag.SourceRef{File: file, Line: 1},
	}}
}

// reverseModel maps a Devin model alias back to its canonical value.
//
// Only the aliases the mapping table actually carries reverse
// (sonnet/opus/haiku, plus gemini). Devin publishes other stable aliases
// (`swe`, `gpt`, `codex`, `claude`) that no canonical model corresponds to;
// those are preserved VERBATIM rather than dropped, because dropping a model
// silently changes which model runs, and a warning records that the value will
// not survive a later compile.
//
// The model identifier is safe to name: it is a published alias, not a secret,
// a payload, or a permission value.
func reverseModel(model, resource, file string) (string, []diag.Diagnostic) {
	if model == "" {
		// Plan, models: "Reverse ingestion leaves an absent model empty."
		// Absence is never replaced with a guess, on any kind.
		return "", nil
	}
	canonical, ok := mapping.ReverseModel(model, mapping.TargetDevin)
	if ok {
		return canonical, nil
	}
	slog.Warn("devin model not canonical", "source", ingest.HarnessDevin, "resource", resource, "model", model)
	return model, []diag.Diagnostic{{
		Code:     diag.CodeIngestDevinModelNotCanonical,
		Severity: diag.SeverityWarning,
		Resource: resource,
		Message:  fmt.Sprintf("%q is a Devin model alias with no canonical equivalent; the value was preserved verbatim", model),
		Hint:     "Use sonnet, opus, or haiku so the model round-trips; other Devin aliases are dropped when the resource is compiled again",
		Source:   diag.SourceRef{File: file, Line: 1},
	}}
}

// --- filesystem helpers ---

func fileExists(src fs.FS, name string) bool {
	info, err := fs.Stat(src, name)
	return err == nil && !info.IsDir()
}

// listSkillDirs returns the skill directories under the skills directory,
// sorted by name.
//
// The membership test is "does <entry>/SKILL.md exist", NOT fs.DirEntry.IsDir.
// That is deliberate and load-bearing. Devin follows skill directory symlinks,
// and D10 materializes every managed skill AS a directory symlink into the
// registry cache ("Skills use directory symlinks"), so a Devin root produced by
// this tool contains no real skill directories at all. fs.ReadDir reports a
// symlink entry with IsDir() == false because its DirEntry comes from the
// readdir record rather than from following the link, so an IsDir check would
// make the ingester unable to read its own output — while a plain file cannot
// slip through, since `notes.txt/SKILL.md` never exists.
//
// Requiring SKILL.md also keeps a skill's own support-file subdirectory from
// being mistaken for a sibling skill, and sorting makes the resource order
// independent of readdir order.
func listSkillDirs(src fs.FS, r root) ([]string, error) {
	dir := r.join(skillsDir)
	entries, err := fs.ReadDir(src, dir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		candidate := path.Join(dir, entry.Name())
		if fileExists(src, path.Join(candidate, skillFile)) {
			dirs = append(dirs, candidate)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// listSubagentFiles returns the flat agents/<name>.md profiles, sorted, and the
// directory-form profiles that were found but not ingested.
//
// Like listSkillDirs it classifies by what exists rather than by
// fs.DirEntry.IsDir, because D10 materializes flat subagents as FILE symlinks
// ("flat subagents use file symlinks") and a symlink's DirEntry reports neither
// kind faithfully. `<name>.md` that resolves to a readable file is the flat
// form; `<name>/AGENT.md` that resolves to a readable file is the directory
// form. Anything else is not a subagent profile.
func listSubagentFiles(src fs.FS, r root) (files []string, directoryForms []string, err error) {
	dir := r.join(agentsDir)
	entries, err := fs.ReadDir(src, dir)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		candidate := path.Join(dir, entry.Name())
		if strings.HasSuffix(entry.Name(), subagentSuffix) && fileExists(src, candidate) {
			files = append(files, candidate)
			continue
		}
		if nested := path.Join(candidate, subagentDirFile); fileExists(src, nested) {
			directoryForms = append(directoryForms, nested)
		}
	}
	sort.Strings(files)
	sort.Strings(directoryForms)
	return files, directoryForms, nil
}

// isNotExist reports whether err means "this optional path is simply absent".
func isNotExist(err error) bool {
	return err != nil && errors.Is(err, fs.ErrNotExist)
}
