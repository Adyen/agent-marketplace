# Target Profiles: Curated Packages for Non-Agent Apps

Status: brainstorm outcome, revised after adversarial review, inspection
of four existing Herdr app sources and comparison with the herdr-plugins
proposal on 2026-11-13; not yet approved for implementation. The normative
contract will live in OpenSpec changes (see Delivery plan); this document
records the agreed direction and the reasoning behind it.

The work is planned for CLI release v6. The consumer configuration format
stays `apiVersion: agent-marketplace/v4`, so no conversion is needed.
An executable, scratch-only Herdr app spike ran on 2026-11-13. It is not
approval to implement the design: the profile skeleton below remains
illustrative, and the spike neither freezes v1 nor implements generic target
profiles.

## Problem

Tools such as the Herdr app (mods, layouts), Herdr (plugins), tern
(plugins) and omp (plugins) install extensions directly from GitHub or
npm. Nobody vets that code, and there is no internal place to say "these
are the extensions we trust". Agent-marketplace already solves this for
agent resources: curated registries, Marketplace mirroring, analysis
assessments, revocation, exact locks, owned-file materialization and drift
checks. We want the same pipeline for other apps, starting with the Herdr
app, and we want app support to be addable without changing
agent-marketplace for every new tool.

Individuals are first-class users. Everything must work without an
organization layer; organization policy only adds restrictions or removes
prompts.

## Non-goals

- Preventing installs that bypass agent-marketplace (`herdr plugin install
  owner/repo`, copying files by hand). That needs host-side policy upstream
  or endpoint scanning; both are out of scope.
- A time-based quarantine window. Curation is manual: security vets a
  package and adds it to the curated registry, as with skills today.
- Translating host packages between apps. Translation stays a compiled
  agent-harness capability.
- Verifying a package's runtime dependencies beyond a PATH lookup.
- Limiting what a package does at run time. The Herdr app runs mods as
  trusted code with unrestricted commands and network access and has no
  per-mod permission setting, so neither the host nor agent-marketplace
  can hold a mod to a declared permission list. Packages carry no
  permission manifest (Decision 7).
- Repository-backed runtime installations for Herdr app packages. Keeping
  a whole repository snapshot does not make it an installed dependency.
- Running upstream install scripts, rewriting mod code or substituting
  machine-specific values into layouts. Authors supply valid native
  packages; users perform any declared manual configuration or activation.
- Changing the consumer `apiVersion`.
- Changes to the Herdr app or Herdr upstream. The design works with what
  they ship today.

## Spike findings (2026-11-13)

A throwaway Go spike tested whether one data-only `herdr-app` profile and
curator import declarations can normalize selected real sources, place them
with ownership and collision refusal, and run `herdr-app check`. It answers
only that feasibility question for one target. The primary source is the
[spike README](../../internal/prototype/targetprofile/README.md) on branch
`spike/target-profiles`, commit `4797afd88`; that link names a spike-branch
file, not a file promised on `main`. The local scenario log is disposable.

| Check | Result |
|---|---|
| Placement | 11 packages placed in a clean scratch home; 9 placed and 2 blocked as unowned collisions in a scratch home seeded with hand-installed pane-switcher and cos-queue. |
| Conflicts and ownership | Two new packages claiming the same files were both blocked in either selection order. An installed owner kept its projection when a newcomer claimed its files. |
| Import refusal and drift | Five negative probes (missing mod, symlink escape, destination outside the kind, shipped name, unsupported fragment) and a changed registry payload were refused. A modified deselected owned file was kept as drift while unchanged owned files were pruned. |
| Host check and runtime | Ten packages with mods passed initial-load checks; the theme had no host verification rule. `andrew-links` passed `herdr-app check` but Python `import helper` failed with `ModuleNotFoundError: file_link`; Chris's bundled `file_link.py` imported successfully. Queue placeholders also survived `herdr-app check`. |

The spike reused `fsboundary.SourceFS` for source reads without a fallback.
Its destination writes used a simplified scratch-only ownership and
filesystem path, not production ownership or materializer integration. It
did not test consent, authenticated assessments, root-bound state, a crash
journal, race protection, or destination symlink safety. Its sidecar hashes
were not authenticated, metadata identity was incomplete, and `requires`
matching was simplified. Profile roots, platforms and detect fields were
parsed but not enforced; hardcoded Herdr registry paths, kind checks and
layout-reference parsing leave second-target generality unproven. Per-file
renames were shown only on one filesystem, not package-atomic publication,
cross-filesystem behavior, or hot reload. The CLI accepts arbitrary paths,
so use it only with scratch homes. The host check loads code and may write
gpui-shell runtime data outside the scratch home; a scratch target is not a
sandbox or a no-side-effect guarantee. These are untested parts of the
design, not proven design flaws.

The prototype admitted the incomplete links and queue packages to expose
false confidence from initial-load checks. Neither is an acceptable
curated package as tested: curation must repair the missing packaged
dependency and unresolved queue values before admission. The checks do not
prove runtime readiness for all packages; the spike did not test real-window
activation or hot reload. It did not rewrite package code or run an upstream
installer.

## Decisions

### 1. One target model: harnesses are targets

A **target** is an app agent-marketplace writes into: `claude`, `devin`,
`herdr-app`, `tern`, ... Today's `target.Definition` already carries roots
and capability flags; it gains **the kinds the target accepts** and, per
kind, a **delivery mode**:

| Mode | Meaning | Examples |
|---|---|---|
| translate | canonical IR compiled to the native format | skills, commands, MCP servers for agent harnesses |
| place | package files copied into the target root, plus owned-entry merges into shared files | Herdr app mods and layouts, tern plugins |
| register | place files in an agent-marketplace store, then register them through the host's CLI | Herdr plugins, omp plugins |

A target may mix modes per kind (omp is a translate target for skills and a
register target for plugins).

**Fan-out:** a resource is delivered to every enabled target that accepts
its kind. A skill reaches every enabled harness; `herdr-app/mod` reaches
only `herdr-app`. A host kind resolves only when its target is enabled
(Decision 10):

- A direct selection of a host package whose profile is not selected is a
  configuration error, reported before acquisition.
- A collection member whose target is not enabled is skipped and listed in
  `status`, without a diagnostic on every sync. This lets organization
  collections include app packages without writing into the homes of
  people who do not use that app.

A kind becomes portable only when two targets declare a translator for it.
Host kinds get no canonical IR until then.

**Cost:** invariants that assume every target accepts every agent kind
(the per-target compile-every-kind check documented in
`internal/harness/roots.go`, per-target hook/plugin/model catalogs, golden
iteration over all compiler targets) change to "every kind a target
accepts". This is the bulk of the foundational work and ships first.

### 2. `targets:` in consumer configuration

```yaml
apiVersion: agent-marketplace/v4
targets: [claude, devin, herdr-app]
```

`targets:` is the same list as `harnesses:`. `harnesses:` stays as an
alias and is deprecated later. A file declaring both is refused.
Resource-group narrowing (`targets:` inside a group, formerly
`harnesses:`) gets the same alias and keeps its "narrows, never widens"
rule.

**Rollout.** v4 parsing rejects unknown fields, so a released v5.x client
refuses a file that contains `targets:`. In a committed repository
configuration that would break teammates on an older CLI. The switch
therefore follows the read-before-write rule:

1. v6.0 reads both spellings. Editors keep the spelling a file already
   uses and write `harnesses:` into a file that has neither.
2. A later release makes editors write `targets:` by default.
3. A release after that deprecates `harnesses:`.

CLI releases older than v5 are not a compatibility constraint for this
change.

**Target IDs stay statically valid.** Every ID in the list must be a
compiled target or the ID of a `target-profile/<id>` selected in the user
or organization layer. Anything else is a configuration error before
acquisition, so the rule that the CLI understands every configured target
(ADR 0010) keeps holding and a typo is still detected.

A target that declares no repository root (Herdr app, tern) accepts
selections only from user and organization layers; selecting its kinds at
repository scope is refused with a diagnostic.

### 3. Target profiles: targets as data

A **target profile** defines a target that uses place and/or register
delivery, without Go code. Translate delivery stays compiled. Built-in
harnesses remain compiled targets. The Herdr app is the first profile; it
ships from a built-in source and goes through the same selection, lock
and approval path as a profile from any other source, so the format and
the external path are exercised by real use from the start.

**One profile per target.** All Herdr app mods and layouts use the same
`herdr-app` profile. Its revisions define the host contract: accepted
kinds, permitted destinations, aggregate handling and host commands.
Different repository layouts do not create profile variants. Package
discovery and source-to-destination mappings belong to package metadata
or curator-supplied import declarations (Decision 4), and cannot widen
the profile's permissions.

Profile skeleton (field names illustrative, fixed in the OpenSpec change):

```yaml
apiVersion: agent-marketplace.io/target-profile/v1
id: herdr-app
label: Herdr app
platforms: [macos, linux, windows]
detect: { any: [{ binary: herdr-app }, { root: user }] }
roots:
  user:
    env: HERDR_APP_HOME
    default: { macos: ~/.config/herdr-app, linux: ~/.config/herdr-app }
host:
  binary: herdr-app
  version:  { probe: [herdr-app, --version], pattern: '(\d+\.\d+\.\d+)' }
  verified: [{ range: ">=X.Y.0 <X.Z.0", date: 2026-11-12, evidence: <ref> }]
  shipped:  { mods: [tabs, agents, files], layouts: [main, terminals, win98] }
package:
  metadata: agent-marketplace.yaml                      # curation sidecar
  display:  { readme: RECIPE.md, preview: preview.png }
kinds:
  mod:
    deliver: place
    place:   { into: [mods/] }
    verify:  [herdr-app, check, "{path}"]
  layout:
    deliver: place
    place:
      into: [layouts/, mods/, backgrounds/, themes/]
      files: [gitlab.json, review-prompt.md, rereview-prompt.md]
  theme:
    deliver: place
    place:   { into: [themes/] }
  icons:                                  # with aggregates (Decision 5)
    deliver: place
    place:   { into: [program-icons/] }
```

Place delivery copies the normalized package payload to the same relative
paths under the target root, except aggregate fragments (Decision 5).
Curation metadata and declared display files are not payload. `into`
lists permitted top-level directories; `files` permits exact root-level
support files. A payload outside those declarations is refused as a whole,
at Marketplace ingestion and at sync, instead of being dropped silently.
Support-file declarations do not authorize replacing unowned files or
writing shared aggregates as whole files. `verify` is optional (Decision 6).

`theme` and `icons` are kinds of their own, so a color theme or a set of
program icons can be shared without a layout. A layout may still bundle a
theme that only it uses. `icons` places its files and adds entries to
`programs.json`, so it ships with the aggregates change (Decision 5).

`host.shipped` lists the mods and layouts the host itself installs for
the verified range. The Herdr app records them in `installed.json` in its
root and replaces them on every start. Ingestion and the author dry-run
refuse a package that claims one of these names (Decision 4).

Herdr app delivery is a **self-contained native installation**. Its
package-owned code and assets must work from their installed paths,
without reading an upstream checkout or relying on repository symlinks.
A layout may bundle mods and declared support files that only it uses,
and requires the packages it shares with other layouts (Decision 4).
Runtime data, host-provided facilities and programs found on PATH remain
separate from those packaged dependencies.

Register-mode example, using the real Herdr CLI (`plugin link <PATH>`,
`plugin unlink <ID>`, `plugin list --json` returning `result.plugins[]`
with `plugin_id` and `plugin_root`):

```yaml
id: herdr
host:
  binary: herdr
  platforms: [macos, linux]
kinds:
  plugin:
    identity: { file: herdr-plugin.toml, format: toml, field: id }
    deliver:  register
    register:   [herdr, plugin, link, "{path}"]
    unregister: [herdr, plugin, unlink, "{id}"]
    inventory:
      command: [herdr, plugin, list, --json]
      items: result.plugins
      id: plugin_id
      path: plugin_root
```

The format must also fit tern without special cases: tern places
`<config>/plugins/<id>/` with per-OS roots (macOS `~/Library/Application
Support/Tern`, Linux `$XDG_CONFIG_HOME/tern`, Windows `%APPDATA%\Tern`) and
a `TERN_CONFIG_DIR` override, and treats dot-named entries as its own
staging. Keep the paper test against real Herdr app layouts, tern and omp,
and add an executable second place-target trial before freezing v1. Tern
is a candidate; that trial has not run. It must prove OS root overrides
and paths with spaces, per-package native directory topology and metadata
exclusions using declarative data, without a target-ID switch. Herdr layout
dependency checks are host-specific author/analysis validation, not a
generic place-engine feature. The trial must not introduce arbitrary
code-executing validation to make placement work.

#### Engine-owned guarantees (profiles cannot relax them)

- Nothing in a profile takes effect before that profile revision is
  approved (Decision 7).
- Never overwrite unowned files. This also protects an app's shipped
  files (for example Herdr's built-in mods), so sync needs no reserved
  name list. `host.shipped` only serves the earlier check at curation.
- Writes stay inside the declared root, with existing `fsboundary`
  symlink containment. Roots resolve under `$HOME` or the OS/XDG config
  and data directories; arbitrary absolute roots are refused. A root may
  not be `$HOME` or an XDG base directory itself, and may not overlap
  agent-marketplace's own directories.
- A profile cannot claim another target's id or overlap its roots.
- No dot-prefixed staging siblings inside target roots.
- Lock, drift, `sync --check`, backup and pruning behave as for every
  other target.

### 4. Herdr app packages in the curated registry

#### Source import and package boundaries

Git origin, ref and optional subdirectory identify the source. An author
or curator declares package boundaries and explicit file/tree mappings
from that source to native destination-relative paths. The declaration
may live alongside upstream content or in curator-supplied metadata;
upstream repositories need not adopt the curated registry layout.
The metadata syntax is fixed in the OpenSpec change, not by adding
per-repository target profiles.

This supports a directory of native mods, a single mod, or a complete
personal app setup. For example, an import can map `mods/chris-links/`
to the same native path, while a source rooted at `mods/` maps
`andrew-tabs/` to `mods/andrew-tabs/`. A complete setup declares its
layouts, bundled mods and permitted root-level support files as one
package. Root files such as `gitlab.json` and review prompts receive
the same ownership and collision protection as other placed files.
`keys.json` remains an aggregate fragment, with merging deferred to
Decision 5.

All mapped inputs must stay within the selected source root, including
through symlinks. The installed package must also contain its declared
package-owned dependencies at usable native paths. Selecting a larger
source root can make a dependency available for packaging, but does not
make a runtime reference to an uninstalled repository sibling valid.
The importer neither follows such references outside its source root
nor rewrites code to repair them.

Import declarations select payload, display content and aggregate
fragments explicitly. Repository development files and installers need
not become payload, but every selected payload file must have an allowed
destination. Imports preserve payload bytes; authors or curators make
any necessary code changes before review. Sync never executes
`install.sh`, `make link` or equivalent upstream installation logic.

#### Curated representation

After import, vetted packages are stored verbatim as reviewed, next to
a curation sidecar. This is the normalized registry representation, not
a required upstream repository layout:

```
herdr-app/
  mods/<owner>.<name>/
    agent-marketplace.yaml    # curation sidecar, written by the curator
    mods/<name>/main.js, mod.json, ...
    keys.json                 # optional fragment (Decision 5)
    RECIPE.md, preview.png
  layouts/<owner>.<name>/
    agent-marketplace.yaml
    layouts/<name>.json, <name>.svg
    mods/<dep>/...            # bundled mods only this layout uses
    backgrounds/..., themes/...
    gitlab.json              # optional, explicitly permitted support file
    review-prompt.md, rereview-prompt.md
    RECIPE.md, preview.png
  themes/<owner>.<name>/
    agent-marketplace.yaml
    themes/<name>.json
    RECIPE.md, preview.png
  icons/<owner>.<name>/       # with aggregates (Decision 5)
    agent-marketplace.yaml
    program-icons/<name>/*.svg
    programs.json             # fragment
    RECIPE.md
```

What security reviewed is byte-for-byte what is stored and served.
Upstream folders carry no agent-marketplace metadata, so the curator adds
the **sidecar**: name, description, tags, `metadata.requires` (the
packages it needs, below), the programs it runs and the host versions it
was tried on (Decision 8), platforms, upstream URL and commit,
`cloned_from` when it started as a copy of a shipped mod (Decision 9),
import mapping/provenance, manual setup requirements and the digest of
the reviewed files. Display files such as `README.md`,
`RECIPE.md` or `preview.png` are declared explicitly. The sidecar and
declared display files are shown by the Marketplace UI and never placed.
The assessed package identity covers the delivery mapping as well as
the payload, so changing a destination requires a new assessment.

#### Dependencies between packages

A package declares the other packages it needs with `metadata.requires`
in its sidecar. This is the existing resource-prerequisites contract: a
list of `kind:name` strings or maps with `name`, `kind` and optional
`registry` and `path` hints, met by any selected resource with that kind
and name. A layout that uses someone else's mod requires it:

```yaml
metadata:
  requires:
    - herdr-app/mod:andrew-tabs
```

Reuse needs three extensions, fixed in the OpenSpec change:

- Host kinds are accepted as prerequisite kinds and printed in warnings
  and remediation. Today an unknown kind is dropped with
  `catalog.requires.invalid`, which is also what a released client does
  with a host kind.
- The sidecar becomes a declaration site. Today only the front matter of
  skills, commands and subagents is read.
- A requirement on a host package whose target is not enabled follows the
  collection-member rule in Decision 1.

Selection follows the prerequisite stack that is open as pull requests
#626, #643, #661, #671 and #672 (state on 2026-11-13) and inherits its
rollout:

- Without `requires.expand`, a requirement is advisory. Selecting a layout
  warns about each unselected mod and prints its install command.
- With `requires.expand: true`, the resolver selects required packages
  transitively from configured Git and directory catalogs (#626) and
  reports them in `install`, `list` and `status` (#643).
- For Marketplace sources the stack only reads requirements (#661). A
  match is previewed and reported as `unrecorded`; recording it in the
  lock waits for the release that makes expansion the default. Until
  then a Marketplace user selects the required mods explicitly, or the
  curator publishes a collection holding the layout and its mods.
- The "Requires" and "Required by" sections in the TUI (#671) and on the
  Marketplace resource page (#672) show host packages without
  target-specific code.

A layout placed without a required mod does not work. Package views
report an unmet requirement next to pending manual setup, never as ready
(Decision 11). Whether such a layout is placed at all is an open item.

`requires` has no version constraint. Keeping a layout and the mods it
requires compatible within one registry revision is the curator's job.

Programs a package runs (`gh`, `python3`) are not `requires` items; see
Decision 8.

#### Destination collisions

A destination path has one owning package. A mod that several layouts
use is its own package, and those layouts require it instead of each
bundling a copy. A package still bundles the mods and support files that
only it uses.

- Two packages that claim the same destination path conflict, even when
  their files are byte-identical.
- A package that claims a mod or layout name in the profile's
  `host.shipped` list is refused at ingestion and in the author dry-run.
- Conflicts are resolved across the complete desired package set before
  publication, independent of package iteration order, with a diagnostic
  naming the conflicting packages and paths. An installed package keeps
  its complete installed projection. A new package, or a new revision of
  an installed one, that claims a path owned by another package installs
  nothing. Two new packages that conflict with each other both install
  nothing. Independent packages still proceed.

Placing a layout and the mods it requires is several filesystem writes,
and a running host can observe the state in between. Publication and
hot-reload behavior still depend on the staging item under Open items.

Upstream resolves a name clash by renaming the incoming mod. Curators do
the same at curation time, or split a shared mod into its own package;
this is why packages are stored "as reviewed" and not "as upstream".
Importing a complete personal setup whose mods already exist as packages
needs the same curator choice: require the existing package, or rename.

Sharing byte-identical bundled trees between packages was considered and
deferred. It needs multi-owner pruning, journaled shared ownership and
conflict handling across connected groups of packages, and it only helps
when sources nobody curates bundle the same mod.

#### Existing-source acceptance examples

These sources were inspected on 2026-11-13. They define the shapes the
import design must handle, not a claim that their current contents are
already curated or installable:

| Source | Finding | Intended treatment |
|---|---|---|
| ai-configs: resources/herdr-app | `pane-switcher` contains native `mods/pane-switcher/` payload plus a README and installer. The installer also binds keys and opens a kept window in layouts. | Import the native payload and declared documentation. Report bindings and kept-window activation as manual setup in v1; do not run the installer or silently claim the setup is complete. |
| ai-configs: enqueue/herdr | The queue bundle has native mod/layout paths, but its layout contains `@@PYTHON@@`, `@@SCRIPT@@` and `@@COS_HOME@@`; `cos_queue.py` is outside the selected subtree. | The author must fix packaging and configuration. Placement alone cannot make this a working bundle; the engine does not substitute paths, acquire the missing script or add register behavior to repair it. |
| chrisj/herdr | A complete setup contains mods, a layout, keys, `gitlab.json` and review prompts. The layout contains blocks needed by the pollers. | Positive model for a complete native package: import the layout, mods and explicitly permitted support files together. Treat keys as aggregate advice until merging ships, and report the manual layout activation. |
| andrewz/herdr-plugins: mods | Native mod folders can be mapped, but `andrew-links/helper.py` imports `plugins/file-link/file_link.py` outside the selected root and depends on checkout topology. | Refuse that incomplete package; independent self-contained mods remain eligible. The author must bundle the dependency at a supported native path and fix the reference. Archiving the entire repository alone does not repair installation. |

These are place-delivery packages, followed by optional verification.
A missing runtime dependency or unresolved setting can survive a host
syntax/load check and fail only during use. The UI distinguishes files
placed, checks passed and manual setup still required. It does not infer
functional readiness from successful placement.

The spike confirmed both false positives: `andrew-links` passed the host
initial-load check although its installed Python helper could not import
`file_link`, and the queue placeholders survived the same check. It
deliberately admitted these incomplete sources to expose the gap; they are
not acceptable curated packages until curation repairs the missing
packaged dependency and queue values. Manual setup for activation,
keybindings or machine configuration does not replace missing
package-owned code. Static checks must not claim to find every dynamic
reference or every placeholder syntax.

### 5. Owned-entry merges into shared app files (second Herdr app change)

Not part of the first Herdr app delivery; mods, layouts and themes come
first. This is the next change after it and ships before register
delivery: several inspected mods are not usable without their bindings,
and the Herdr app needs no register mode. Until it ships, sync places
nothing in `keys.json` and reports a package's suggested bindings as
advice.

Profiles declare a shared file the way compiled targets declare MCP
configuration. The existing ownership record is a container plus a key
(`mcpServers` plus the server name), which maps directly onto a
`keys.json` section plus key:

```yaml
aggregates:
  keys:
    file: keys.json          # relative to the target root
    format: json             # an existing format-preserving patcher
    fragment: keys.json      # package file that supplies entries
    containers: ["global", "terminal", "layout:*", "mod:*"]
    on_edit: cede            # or restore
  programs:
    file: programs.json
    format: json
    fragment: programs.json
    containers: ["programs"]
    on_edit: restore
```

The merge reuses the existing owned-entry rules: ownership is proven from
durable state, an unmanaged entry with the same identity is preserved and
only the generated entry is refused, and deselection prunes only owned
entries. The engine needs two additions:

- A generic entry kind. Today's kinds are MCP server and hook.
- A container allowlist declared by the profile, because containers come
  from the package here instead of a fixed vocabulary.

`on_edit` chooses what happens to an owned entry the user changed:

- `restore` is today's MCP rule: sync puts the configured value back.
- `cede` hands the entry to the user. It suits key bindings, where
  `restore` would overwrite a rebinding on the next sync. A reset or
  deleted file (the Herdr app escape hatch moves `keys.json` aside) cedes
  its owned entries too. The exact drift and re-add rules are fixed in the
  OpenSpec change, which amends `owned-entry-reconciliation`.

`programs.json` is the second Herdr app aggregate. Its `programs` map is
keyed by program name, and the `icons` kind adds entries there that point
at the icon files it placed. Its `shells` and `interpreters` lists are
not keyed containers and stay unmanaged.

A package may consist of fragments only, such as a shortcut pack without
a mod. Whether that is a kind of its own is fixed in the OpenSpec change.

The Herdr app treats any edit to `keys.json` as making the whole file the
user's, and then stops adding new default shortcuts on upgrade. Without an
upstream change the first merge can only warn about this, once.

### 6. Host commands: register lifecycle and verification

Register lifecycle:

1. **Stage** into an agent-marketplace store, one directory per revision:
   `<data>/targets/<target>/<id>/<revision>/`. Never into the host's own
   directory. Upgrades add a directory and re-register, so running host
   processes keep their files.
2. **Inventory and classify ownership** before changing a registration.
   An absent ID permits installation. An existing ID permits an upgrade
   only when durable ownership proves that it points to the exact
   previously recorded generation. Every other registration blocks the
   operation and remains untouched, even if its path is under our store.
   Being under the store is not ownership proof.
3. **Journal** the previous and desired registration paths, generation
   references and the approved profile revision that supplies the
   commands before running the transition.
4. **Register** with the declared argv. Failure leaves the staged
   generation and journal pending and reports it. A later mutating sync
   inventories the host before deciding whether to retry.
5. **Confirm and commit ownership** only after live inventory identifies
   the exact desired generation path. ID presence alone does not prove
   an upgrade succeeded. Owned and missing is drift; an unexpected path
   is a conflict.
6. **Unregister** deselected owned registrations only after live inventory
   confirms the exact recorded path and the removal is journaled.
   Confirm absence before committing removal and allowing generation
   pruning; preserve any generation still referenced elsewhere.

Recovery distinguishes the recorded previous state, the desired state
and an unexpected state. The previous state permits retry; the desired
state permits completing the journal without repeating the command. An
unexpected registration stops the transition for repair, without
unlinking it or overwriting it through a retry. Missing registrations
may be repaired only while the package remains selected and its
execution authority is valid.

Each register profile documents the host command's collision behavior.
Agent-marketplace serializes its own transitions for a target, but
inventory before a command cannot prevent another process from changing
the registration between those steps. A host that lacks a conditional
update API cannot provide that stronger guarantee; post-command
inventory detects observable divergence and preserves pending evidence
instead of attempting a destructive rollback.

**Verification.** A kind may declare `verify`, a command that checks one
delivered package, such as `herdr-app check <mod folder>`. It runs after
place or register in a mutating sync. A failure is reported, makes sync
exit nonzero and is recorded. Whether a failed package stays in place
remains open for the OpenSpec change. The prototype leaves files placed
after a failed check, but the scenario did not exercise that case and this
behavior is not a policy choice.
Herdr app mods use place followed by verify, not register. Verification
reports only what the host check exercised; it does not prove all runtime
paths or user configuration are valid, and it does not repair them.

Keep distinct results for files placed, the named check and its scope,
exact package/profile revisions and host version, pending manual setup or
unmet requirements, and runtime acceptance evidence only when actually
exercised. The host documents `herdr-app check` as an initial-load and eager
element check, not a check of deferred renders or later runtime paths.
The spike demonstrated the gap: `andrew-links` passed the check but its
Python helper import failed, while queue placeholders also passed.
Never infer functional readiness from a
successful host check, no warnings, or an empty `manual_setup`. Host-version
evidence and runtime witnesses are curator evidence, not a new
consumer-configured command surface.

When host commands run:

| Command | Runs |
|---|---|
| mutating `sync` | version probe, register, unregister, inventory, verify |
| `sync --check`, `status`, TUI drift views | nothing; they report the recorded inventory and verify results |
| `sync --check --verify`, `doctor` | version probe, inventory and verify, live; never register or unregister |
| author import dry-run | nothing; discovery, validation and placement planning only |
| explicit author live verification | approved version probe and verify against the selected installed package; never register or unregister |

The default read-only path executes nothing for two reasons. Those
commands run unattended in CI, pre-commit and session-start hooks. And a
verify command can load package code: `herdr-app check` loads the mod. The
cost is that an out-of-band `herdr plugin unlink` shows up only at the
next mutating sync or live check. The profile declares the command; the
user decides when it runs outside a mutating sync.

Safety rules:

- No host command runs before the profile revision is approved
  (Decision 7).
- Every argv must start with `host.binary`. No shell, no other programs.
- The binary is resolved to an absolute path at approval and recorded
  with it. Later runs execute the recorded path, not a fresh PATH lookup.
  A path inside the current repository, or reached through a relative PATH
  entry, is refused. A changed path asks for approval again.
- Templates substitute only `{path}`, `{id}`, `{version}`, each as exactly
  one argv element.
- Stdin closed, per-command timeout, bounded and redacted output.
- Journal recovery uses the exact previous and desired registration
  states above; it never treats ID presence alone as success.
- A missing host binary makes the target unavailable for that run; other
  targets proceed.

### 7. Consent: profile approval and package assessment

Every profile revision needs approval before it takes effect, whatever
its delivery mode. Today the binary decides where a source's content
lands, and Marketplace analysis judges a resource knowing that
destination. A profile moves that choice to the source, so reviewing a
package only means something together with the profile revision that
places it.

**Assessment binds the package to its profile.** The Marketplace records
the exact profile revision digest used to interpret and assess each host
package, as part of authenticated assessment identity. Before delivery,
the client verifies both the exact package identity and that its selected
profile matches this digest. The same check applies to online resolution
and offline delivery from cache; missing or mismatched bindings refuse
the affected delivery.

Profile approval authorizes destinations and commands. Package assessment
authorizes the package under that profile; neither substitutes for the
other. A profile update needs a matching assessment before a Marketplace
package can use it. V1 binds the full profile digest, accepting that even
an evidence-date change requires reassessment. A narrower digest of
delivery behavior is deferred until that cost justifies a compatibility
contract.

Git and local-directory packages retain their existing source trust
rules. They cannot inherit a Marketplace assessment made under a
different profile. Marketplace ingestion must select the assessment
profile explicitly and preserve its digest through publication and
client verification.

**Assessment is review, not a sandbox.** The Herdr app's guide (checked on
0.1.15) says mods are trusted code whose commands and network access,
including `fetch`, are unrestricted. `mod.json` has no permission field
and the app has no per-mod setting. A mod can start any program through
`exec`, so a list of what it calls, derived from its source, is
incomplete by construction: arguments can be computed, `sh -c` runs
anything, and a helper script in another language is outside the
analysis. Packages therefore declare no permissions, and nothing in this
design claims to limit a package at run time.

The pipeline provides three things instead:

- The bytes that were reviewed are the bytes that are delivered.
- Analysis produces a capability summary: whether the package runs
  programs, reaches the network or reads files outside its own folder.
  These are the three things the Herdr guide tells an installer to
  report before adding a mod. The summary is shown to reviewers and
  users as disclosure and is labeled as analysis output.
- A package that turns out to be harmful is revoked.

**Local approval binds resolved destinations.** Approval is recorded in
user state against the profile revision digest, canonical resolved
destination roots and resolved host executable path, when applicable.
The first sync of a target shows what the profile does: its roots, place
rules, aggregate files, the exact command set and the resolved host
binary. A new profile revision or a change to either resolved roots or
executable path shows a diff and requires approval again.

Changing `HERDR_APP_HOME`, XDG settings or a root alias can change the
destination without changing profile bytes. Until the new destination is
approved, the target reports "target destination changed" and writes
nothing at either location. Ownership stays bound to the recorded
installation roots. Approval of the new location allows a new
installation there; it does not authorize cleanup of the old one.
V1 does not automatically relocate installations: status shows both
locations, and retiring the old installation requires an explicit action
using its recorded ownership and execution authority.

Headless sync skips an unapproved target with a remediable diagnostic;
other targets proceed, so one pending approval never blocks agent
resources. An organization layer may pre-approve specific profile digests
with explicit destination and executable-binding rules for hands-off
rollout. A digest alone does not authorize arbitrary environment-derived
destinations. The client records the concrete approved bindings on each
machine; a binding outside those rules requires interactive approval.
Without organization preapproval, individuals approve interactively.
Enabling a target is not by itself consent.

### 8. Conditions on the host machine

| Condition | Declared by | Check | Effect |
|---|---|---|---|
| Platform | profile, package sidecar | runtime OS | hard: skip elsewhere |
| Host present | profile `detect` | PATH lookup (no execution) or root exists | hard: target unavailable this run |
| Host binary (register) | profile `host.binary` | PATH lookup, absolute path recorded | hard |
| Host version | profile `host.version` | single probe with fixed argv, run only when host commands may run (Decision 6) | soft: warn outside `verified`; organization policy may block |
| Package host version | package sidecar `host: { min, tested }` | compared with the last recorded version probe | soft: warn below `min` or above `tested` |
| Required packages | package sidecar `metadata.requires` | a selected resource with that kind and name (Decision 4) | advisory; selected automatically with `requires.expand` |
| Programs the package runs | package sidecar `binaries: [gh, python3]` | PATH lookup only | advisory warning |

Programs a package runs have their own sidecar field. They are not a
`binary` kind inside `metadata.requires`: a `requires` item names a
resource that can be selected, locked and shown as resolved by the
Marketplace, and `sync --strict` fails when one is unmet. A program on
PATH is none of those. A missing program never blocks, also under
`--strict`, because the PATH agent-marketplace sees can differ from the
host's (GUI launches, launchd, IDE terminals). Dependency binaries are
never executed; dependency versions, libraries, OS packages and package
managers are out of scope.

### 9. "Known to work" evidence

Profiles carry `verified` ranges with a date and evidence reference,
following the shipped-catalog evidence pattern. Outside the range, sync
still works and `status`/TUI warn from the last recorded probe
("herdr-app 0.11 is newer than this profile's verified range").

Packages carry the same kind of evidence in their sidecar. `host.min` is
the oldest host version the package works on and `host.tested` the last
one someone tried it on (Decision 8).

A package that started as a copy of a mod or layout the host ships
records `cloned_from` with the shipped name and host version, as the
Herdr app does for its own clones. The host never updates a clone, so
package views show the origin and note when the installed host is newer
than the copy.

### 10. Where profiles come from

Origin and distribution are separate. A profile is a virtual resource
kind, `target-profile/<id>`: like a collection it is selected and locked,
and it installs no files of its own. It can originate in agent-marketplace
(the built-in source), the upstream app's repository (preferred end state:
the profile versions with the app), an internal team's repository, or a
local directory while authoring. It is distributed through any configured
Git, directory or Marketplace source, pinned in the lock like any
resource, and resolved before the resources that need it.

People writing their own profiles is a main goal, so external profiles
are supported from the first profile change, not added later.

**Enabling a target** is one action: selecting its profile. Selection
appends the profile's id to the selecting layer's `targets:` list, and
deselecting the profile removes it. The two can still be edited apart by
hand:

- An id in `targets:` without its profile selected is a configuration
  error (Decision 2).
- A selected profile whose id was removed from `targets:` leaves the
  target disabled; its packages are skipped and `status` says so.

A profile with only a user root takes effect only when selected in the
user or organization layer.

Any configured source may supply profiles. Existing `allowed_registries`
and `allowed_marketplaces` policies restrict sources for organizations
that want only curated profiles; the Marketplace then also acts as the
discovery surface for every profile it serves. Local-directory profiles are
flagged as unvetted in `status`. Approval per revision (Decision 7) is the
gate for every profile, whatever its source.

### 11. Presentation (sketch, not yet discussed in depth)

- Group by destination: today's agent resource view stays as is; each
  enabled target with host kinds gets its own TUI section and web catalog
  tab/facet. Disabled targets are hidden in the TUI and filterable on the
  web.
- Host packages render through one generic package card driven by profile
  `display` metadata (name, description, preview, capability summary,
  owned keys). Adding a target adds no per-target UI code.
- Host kinds are target-scoped names (`herdr-app/mod`), so the agent kind
  list does not grow.
- Package views separate placement and verification results from pending
  manual configuration or activation. A placed package with outstanding
  setup or an unmet package requirement must not be presented as fully
  ready to use. An empty `manual_setup` or successful host check alone
  does not establish readiness (Decision 6).
- Package requirements reuse the "Requires" and "Required by" sections
  that the prerequisite stack adds to the TUI and the Marketplace
  resource page (Decision 4).
- The capability summary is labeled as analysis output, not as a
  permission the host enforces (Decision 7).

### 12. Author dry-run and live verification

Mod authors need a way to check a local source and its import declaration
before submitting it or changing their installed setup. The author
dry-run uses the same importer, profile validation and placement planner
as Marketplace ingestion and client sync. It works without an
organization layer or a running host. CLI spelling is fixed in OpenSpec.

The report shows discovered package identities, selected and excluded
source files, native destination paths, profile revision, invalid or
escaping paths, missing declared packaged dependencies, declared package
requirements, names the host ships, and pending manual setup. When given
a destination, it also reports collisions and proposed ownership changes
against current state without applying them; otherwise it marks
destination checks as not performed. A refused import returns nonzero
with file-specific diagnostics.

Dry-run writes no target files, consumer configuration, acquisition locks
or approval/ownership state. It executes no host commands, mod code,
version probes or upstream installers. Source acquisition may use the
ordinary cache when the author chooses a remote source; a local source
needs no network acquisition.

Explicit live verification is separate and follows Decision 6's approval,
binary binding and execution rules. It exercises the profile's check
against an explicitly selected installed package; it does not implicitly
install, activate or repair that package. Producing `verified` evidence
must record which profile/package revisions and host version were checked.

Source containment, declared dependencies and destination permissions
are enforceable checks. Static analysis can identify some missing
references or placeholders but cannot prove every path constructed by
JavaScript or Python at runtime. The report distinguishes checked,
refused and untested behavior; successful dry-run is not a claim of
runtime correctness. Known incomplete packages such as the queue and
links examples need author fixes before acceptance, even if a generic
static checker cannot discover every defect.

## Compared with the herdr-plugins proposal

The herdr-plugins proposal (Andrew Z, 2026-11-13, not built) covers
sharing for the Herdr app alone:
one internal repository, a `plugin.json` per package, a Python installer
and checks on every merge request. Its package folders have the same
shape as the curated representation in Decision 4, so that repository can
be a source for this design through import declarations.

This design stays the delivery path. It reuses the existing lock,
ownership, drift, backup and revocation machinery, delivers the bytes
that were reviewed, imports sources without changing their layout and
covers more than one app.

| From the proposal | Outcome |
|---|---|
| Shortcuts merged at install | Adopted as ordering: aggregates ship before register (Decision 5). |
| `requires` between plugins | Adopted through the existing `metadata.requires` (Decision 4). It replaces shared bundled trees. |
| `herdr: { min, tested }` and `cloned_from` | Adopted in the sidecar (Decisions 8 and 9). |
| List of names Herdr ships | Adopted as `host.shipped` in the profile (Decision 3). |
| Theme and icons kinds | Adopted (Decisions 3 and 5). |
| `permissions` manifest checked in CI | Not adopted. The host cannot enforce it; the analysis capability summary covers disclosure (Decision 7). |
| Separate installer with its own `installed.json` | Not adopted. It duplicates ownership state agent-marketplace already keeps. |

## Open items

- **TUI and web presentation**, including the approval prompt and drift
  views (Decision 11 is only a sketch).
- **Import and author-tool syntax:** package boundaries, source mappings,
  declared dependencies and manual setup metadata; CLI spelling for the
  dry-run and explicit live verification in Decision 12.
- **Analysis policy for host packages:** capability summaries (network,
  exec, file access outside the package) for gpui-kit JavaScript, Luau and
  scripts; a deliberate policy for prebuilt binaries, which
  `marketplace.find.invalid-utf8` rejects today. Mods can run programs and
  fetch URLs, so this gates the `herdr-app-target` change. Herdr plugins
  need it because `herdr plugin link` skips `[[build]]`, so vetted
  packages must ship ready-to-run contents. The summaries are disclosure,
  not enforcement (Decision 7); the policy must say how they are derived
  and how the UI states their limits.
- **Unmet package requirements.** `requires` is advisory unless
  `requires.expand` is on, but a layout without a required mod does not
  work. Undecided: place it and report it as not ready, or refuse it.
- **Organization preapproval and retirement UX:** the policy syntax for
  allowed destination and executable bindings, and the explicit action
  that retires an installation at a previous root (Decision 7). Apply
  Decision 7's canonical resolved-root binding to ownership, status and
  retirement; never substitute the caller's current `--home`. The spike's
  root-change plan illustrated the missing binding but did not test
  approval.
- **Adopting existing hand installs.** Before release, document either a
  safe first-release refusal and remediation path or design a separate,
  explicit opt-in adoption flow. Never adopt implicitly, even when bytes
  match. Pane-switcher and cos-queue collided with hand-installed files in
  the seeded scratch home. A later adoption flow cannot treat byte equality
  or a path under a store as durable ownership proof; shipping adoption
  itself is not required if safe refusal is the chosen path.
- **Organization block on unverified host versions:** exact policy shape.
- **Profile format paper and executable tests** against real Herdr app
  layouts, tern and omp before freezing v1.
- **Herdr app root on Windows:** not documented by the Herdr app; confirm
  before declaring it.
- **Appending to a layer that declares no target list.** Target lists
  replace across layers instead of merging, so writing
  `targets: [herdr-app]` into a user layer that had none would replace the
  organization's harness list for that user. Proposed: seed the new list
  with the currently effective one. The cost is that later organization
  additions no longer reach that user.

### Carried from the adversarial review, not yet resolved

- **Where the target model lives.** `target.Definition` is in
  `pkg/harness-compilation`, whose charter excludes distribution and sync.
  `ValidateDefinition` rejects a target with only a user root, and there
  are already two registries (`TargetInfoMap`, `InstallationAdapter`).
- **Register store versus package generations.** `plugin-delivery`
  already specifies immutable, garbage-collected generations with journal
  references; the store in Decision 6 should reuse them.
- **Terminology.** "Delivery mode", "plugin" and "package" already have
  glossary meanings in `openspec/config.yaml`. The glossary also needs
  "target" and "target profile".
- **Profile identity and lifecycle.** A built-in `herdr-app` profile, "a
  profile cannot claim another target's id" and "upstream ships the
  profile" cannot all hold. Unregistering after a profile update or
  removal needs the revision that did the registering.
- **Staging and publication.** Compare external same-filesystem staging,
  per-file publication and package publication; test cross-filesystem
  failure and what the app observes during publication. Keep the
  no-dot-sibling and destination-containment guarantees. Define how the
  engine authorizes staging outside target roots while restricting
  publication to those roots. The spike showed per-file renames only on
  one filesystem; it did not prove package atomicity, `EXDEV` handling or
  hot-reload behavior.
- **Herdr replaces its shipped mods on every start.** `host.shipped`
  (Decision 3) refuses known shipped names at curation. A later Herdr
  release may still ship a name we already placed. The guide documents
  that an edited shipped file is moved to `<name>-edited` and replaced by
  Herdr's version; whether a placed package of the same name is treated
  that way is untested. Who wins, and what drift on an owned placed file
  does, is unspecified.
- **Discovery and profile selection.** Package boundaries and mappings
  come from import declarations; accepted kinds and destination policy
  come from the target profile. `list`, locks and Marketplace indexing
  must use the same interpretation. The server's profile-selection
  configuration is still open; its selected revision must be bound to
  the package assessment as specified in Decision 7. Slash-named kinds
  such as `herdr-app/mod` also meet `kind/name` locators and the
  `kind:name` prerequisite shorthand.
- **Required selections** of a host package whose target is not enabled
  or not available on the machine.
- **Windows** is outside the supported platforms in `CODING_STANDARDS.md`,
  while the profile schema and the tern roots include it.
- **Register details:** a package-supplied `{id}` with a leading `-`;
  `herdr plugin enable/disable` state; backup cannot restore host-side
  registration. Exact-path ownership checks and recovery behavior are
  specified in Decision 6.
- **Edited owned files and failed verification.** Decide whether
  deselection or upgrade keeps, restores or cedes a locally edited file,
  and whether a failed check retains placed files or rolls them back.
  Require exact tests for each chosen policy; the spike kept a modified
  deselected file as drift, but that does not choose production semantics.

## Delivery plan (proposed OpenSpec changes, in order)

Before freezing the `target-profile-place` contract, close the executable
second-target trial, publication/crash/symlink and root-identity design,
existing-install refusal or adoption path, initial-check/readiness evidence
contract, and the analysis-policy/assessment-binding gates already called
out below. `generalize-target-kinds` can start independently once the
target-model home, glossary and kind matrix decisions are settled; it need
not wait for the full place feature.

1. `generalize-target-kinds`: per-target accepted kinds and delivery
   modes; invariants and catalogs keyed by accepted kinds. No user-visible
   behavior change for existing harnesses.
2. `targets-alias`: `targets:` read as the same list as `harnesses:`
   under `apiVersion: agent-marketplace/v4`, the static target-ID rule,
   editors that keep the authored spelling, TUI first-launch support.
3. `target-profile-place`: profile format v1, the virtual
   `target-profile` resource kind from built-in and external sources,
   approval bound to profile revision and resolved destinations, explicit
   retirement of previous-root installations, place delivery with
   curation sidecars, declarative source import and self-contained native
   payloads, exact support-file permissions and single-owner
   destinations. Include conflict planning that preserves installed
   projections, sidecar `metadata.requires` with host kinds as
   prerequisite kinds, `verify`, platform/detect/version conditions, the
   sidecar program list and the shared author dry-run.
   Define and implement the exact profile binding and delivery-mapping
   identity in host-package assessments across Marketplace ingestion,
   publication and online/cached client delivery. No aggregate merging.
4. `herdr-app-target`: built-in Herdr app profile with mod, layout and
   theme kinds and the shipped-name list, curated registry layout,
   Marketplace ingestion/analysis of mods and complete app bundles,
   package host versions and `cloned_from`, manual setup reporting,
   explicit author live verification and TUI QA journeys. Exercise the
   four source shapes in Decision 4 through one profile. Needs the
   analysis policy and enforced package/profile assessment binding first.
5. `target-profile-aggregates`: owned-entry aggregates declared by
   profiles, including `on_edit`; `keys.json` and `programs.json` for the
   Herdr app, the `icons` kind and fragment-only packages.
6. `target-profile-register`: register delivery, store layout, live
   inventory and ownership checks before mutation, recorded inventory
   drift, binary binding, journaled previous/desired registration paths
   and exact-state recovery.

Changes 3 and 4 depend on the prerequisite stack (#626, #643, #661, #671,
#672) being merged. Selecting a Marketplace layout together with its
mods in one step also needs the later release that records Marketplace
requirement edges; until then curated collections cover that case.

Later changes cover Herdr plugins and omp (register mode plus binary
analysis policy), tern, the presentation work, writing `targets:` by
default and deprecating `harnesses:`.

The relevant OpenSpec changes must prove these acceptance cases:

- A package assessed under profile P refuses delivery under approved
  profile Q, online and offline, even when package bytes are unchanged.
- An unmanaged same-ID registration survives installation unchanged.
  A failed upgrade that leaves the old generation registered remains
  pending; recovery does not report it as the new generation.
- Destination conflict outcomes do not depend on package iteration order.
  An installed package keeps its projection when another package claims
  one of its paths; two new conflicting packages both install nothing.
  A package claiming a shipped host name is refused at ingestion. An
  unowned file at the exact desired path is not implicitly adopted even
  when its bytes match.
- A layout that requires a mod package warns with the mod's install
  command when expansion is off, and selects the mod from a Git or
  directory catalog when `requires.expand` is on. A host kind in
  `metadata.requires` is not reported as `catalog.requires.invalid`.
- A program listed in the sidecar is never a `requires` item, and a
  missing one does not fail `sync --strict`.
- Changing a root override writes nothing at either location until
  approved. Approval of the new destination does not prune the old one.
- Native mod directories and complete personal app bundles use one
  `herdr-app` profile through explicit import mappings. A complete bundle
  places its layout, mods and permitted root support files together;
  undeclared destinations and unowned collisions refuse safely.
- A mapped input or declared packaged dependency outside the selected
  source root refuses import. Making a larger repository available does
  not permit runtime dependence on an uninstalled checkout sibling.
  Cover the links-helper failure with a fixture that exercises the
  installed Python import, rather than relying on static detection.
- A queue-layout fixture with unresolved placeholders is not treated as
  working merely because placement or a host load check succeeds.
  Known incomplete queue and links packages are refused at curation, not
  accepted on the strength of that check. No engine path substitutes
  values or runs an upstream installer.
- A theme with no verify rule is reported as unverified, not passed.
- Pane-switcher activation, layout selection and deferred key bindings
  remain visible as manual setup. Placement and verification cannot
  clear those requirements implicitly.
- Author dry-run and actual ingestion/placement agree on package
  identity, payload, mapping and refusal diagnostics. Dry-run changes
  no target/config/lock/approval/ownership state and starts no host or
  package process; absent destination checks are reported as untested.
- Explicit live verification requires approval and reports the exact
  revisions and checks exercised without implicitly installing or
  activating packages.
- A second place target uses declarative profile and import data for its
  roots, package topology and metadata exclusions, with no target-ID-specific
  branch.
- Destination symlinks cannot redirect writes or pruning outside approved
  roots. Interrupted publication recovers under the chosen transaction
  contract, and ownership cannot transfer to a different root by changing
  the current environment. These require future annotated witnesses; the
  Herdr spike did not prove them.

Automated fixtures reproduce these source shapes and failures locally;
they do not require access to the example repositories, credentials or
machine-specific paths.
