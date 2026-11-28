# Herdr source fixture inventory

Discovery evidence for I4 only. This is not a supported profile, an install
guide, or proof of runtime readiness.

## Pinned evidence

- The approved design is pinned at `29bf7a6b4e7cd33c673765e5417a033170765720`; [Decision 4, lines 706-972](2026-11-13-target-profiles-design.md#L706-L972) covers import boundaries and source findings, including the table at lines 950-972.
- From a clean checkout, inspect that section with `git show 29bf7a6b4e7cd33c673765e5417a033170765720:docs/plans/2026-11-13-target-profiles-design.md | sed -n '706,972p'`.
- The feasibility spike is `4797afd88e7d2c75f07349272749c43e7e670921`, under `internal/prototype/targetprofile/`. Its profile is `internal/prototype/targetprofile/profiles/herdr-app.yaml`, and its scenario driver is `internal/prototype/targetprofile/run-scenarios.sh`.
- These commits pin the design and spike evidence, not upstream source checkouts. The declarations give source origins, but no upstream commit IDs or payload digests.
- The spike profile is `agent-marketplace.io/target-profile/v1`, `herdr-app`, with a claimed host range `>=0.1.15 <0.2.0` dated 2026-11-13. The README says the existing host must be 0.1.15 or newer. Neither reference records the exact binary used by each scenario.

## Four source forms

### Pane switcher

- Pinned declaration: `4797afd88e7d2c75f07349272749c43e7e670921:internal/prototype/targetprofile/imports/pane-switcher.yaml`. The design identifies `ai-configs` at `resources/herdr-app/pane-switcher`.
- Selected payload: `mods/` to `mods/`. The declaration lists `README.md` for display only; it is not in the mapped payload. The installer is not imported.
- Expected native path: `mods/pane-switcher/`.
- Manual setup: add the installer's Alt-Tab, Alt-A, and Alt-C bindings to the app's key settings, then activate the kept window. The spike did not run the installer or establish this setup.
- Evidence state: the spike README reports that all mod initial-load checks passed. Key bindings and kept-window activation remain untested.

### Queue layout

- Pinned declaration: `4797afd88e7d2c75f07349272749c43e7e670921:internal/prototype/targetprofile/imports/enqueue.yaml`. The design identifies `ai-configs` at `resources/skills/enqueue/herdr`.
- Selected payload: `layouts/` and `mods/`; display list is empty. The declaration names `python3`, which is not evidence that the queue runs.
- Expected native paths: `layouts/queue.json` and `mods/cos-queue/`. The layout retains `@@PYTHON@@`, `@@SCRIPT@@`, and `@@COS_HOME@@`; `cos_queue.py` is outside the selected subtree.
- Manual setup: select the queue layout with Ctrl-Shift-L. This does not repair its unresolved values or missing script.
- Curation failure: treat this source as refused until its author fixes the unresolved values and packages the missing script. The declaration's replacement and layout-switch instructions do not repair the package.
- Evidence state: the spike placed this incomplete source in a clean scratch home, and the README/design report that the placeholders survived `herdr-app check`. No queue run was exercised; this scoped load check is not runtime evidence. A corrected queue is unavailable.

### Chris setup

- Pinned declaration: `4797afd88e7d2c75f07349272749c43e7e670921:internal/prototype/targetprofile/imports/chrisj-herdr.yaml`. The design identifies `chrisj/herdr`.
- Selected payload: `layouts/chris.json`; `mods/` except `test_*` and `*.mjs`; and root files `gitlab.json`, `review-prompt.md`, and `rereview-prompt.md`. `README.md` is display only. `keys.json` is declared as a fragment, not copied; the profile defers aggregate writes.
- Expected native paths: `layouts/chris.json`, mapped `mods/` entries, and the three named root support files.
- Manual setup: switch to layout `chris` and add the bindings from `keys.json`.
- Evidence state: the spike reports placement and a successful Python import for Chris's bundled `file_link.py`. This does not verify the full setup, layout activation, or key bindings.

### Andrew links

- Pinned declaration: `4797afd88e7d2c75f07349272749c43e7e670921:internal/prototype/targetprofile/imports/andrewz-herdr-plugins.yaml`. The design identifies `andrewz/herdr-plugins`; this declaration also lists five other mods, which are not assessed here.
- Selected payload for this fixture: `mods/andrew-links/`, excluding `test_*` and `*.mjs`; display list is empty.
- Expected native path: `mods/andrew-links/`. Its helper imports `plugins/file-link/file_link.py` outside the selected mod.
- Manual setup: the pinned declaration lists none; that absence does not establish readiness.
- Curation failure: refuse this incomplete source until its author bundles the dependency at a supported native path and fixes the import. A larger checkout does not make the sibling available after placement.
- Evidence state: the spike placed it and `herdr-app check` passed, but the separate Python import of the placed helper failed with `ModuleNotFoundError: file_link`. Chris's bundled helper imported successfully. The spike's host check did not test this runtime dependency.

## Evidence limits and I4 handoff

A fresh clone cannot fetch the pinned spike commit from origin. Decision 4 records findings for all four sources, but checking the declarations, the README, and the per-source observations that rely on them requires a copy of the spike commit.

The README at `4797afd88e7d2c75f07349272749c43e7e670921:internal/prototype/targetprofile/README.md` reports 11 placements in a clean scratch home, 9 placements and 2 unowned collisions in a seeded home, and passing initial-load checks for mods. It says the scenario log is disposable and the runner is not an assertion suite. The design records false positives for links and queue; these observations are not runtime tests or proof about current upstream sources.

Scope limitation: the pinned objects omit upstream source snapshots and the scenario transcript, so this inventory cannot reproduce the original checkouts or runs.
