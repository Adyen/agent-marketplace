# Desktop GUI development

Read this before you change or inspect the desktop GUI (`gui/`,
`cmd/agent-marketplace-gui/`, `internal/localapi/`). Use the cheapest loop
that can answer your question, and climb to a more expensive loop only when
the cheaper one can't. Each layer is described here, so you don't need to
reverse-engineer it from the client or harness sources.

## Layers

| Layer | Path | Role |
|---|---|---|
| Vue app | `gui/src/` | `WorkspaceView.vue` is the shell. `controller.ts` holds the view state. `api/client.ts` POSTs every call and reads `/api/events` as SSE `data:` frames |
| Contract | `api/gui-local.yaml` → `gui/src/generated/api.d.ts` | Regenerate with `node scripts/generate-api.mjs` from `gui/`. The drift test fails when the two disagree |
| Local API | `internal/localapi/` | HTTP over the `internal/session` core. Bearer credential from the bootstrap |
| Native host | `cmd/agent-marketplace-gui/` | Wails v2 (cgo). Embeds `frontend/dist` (the Vite `outDir`) and hands the credential over through the `App.Bootstrap` binding |

Behavior authority is `openspec/specs/gui-*` and `session-*`. Presentation
authority is `gui/DESIGN.md`: read its section headings first, then only
the section you're changing. Nuxt UI theme overrides live in
`gui/ui-options.ts`. Tokens live in `gui/src/design-tokens.css`.

## Loops, cheapest first

### 1. See it: mocked preview (seconds, no Go)

For layout, styling, copy, and anything you'd judge from a screenshot:

```sh
cd gui
node preview/shoot.ts                                  # default scenario, light + dark
node preview/shoot.ts --select code-review --theme dark
node preview/shoot.ts --scenario empty --size 1440x900
```

It starts its own Vite server on a free port, captures, and shuts down. It
prints one PNG path per line under `gui/test-results/preview/` (gitignored),
and you open those files with your image-reading tool. No build is needed:
the script renders the current `src/`.

For back-and-forth work (computed styles, hover, clicking through dialogs),
run the persistent server in the background, then drive it with the
Playwright MCP browser (navigate, screenshot, evaluate
`getComputedStyle`) rather than writing a one-off script:

```sh
cd gui && ./node_modules/.bin/vite --config vite.preview.config.ts   # http://127.0.0.1:5199, HMR
```

How the mock works:

- **Scenarios** live in `gui/preview/scenarios.ts`. `default` covers every
  status group and resource kind, plus required org rows, a harness
  override, a long registry name, 7 diagnostics (3 of them share a code),
  and directory/git/marketplace/org sources. `empty` has no rows, sources,
  or diagnostics. Add a scenario there when you need a state the existing
  ones lack. Build it from the `gui/src/fixtures.ts` builders the Vitest
  suite also uses, and `vue-tsc` will catch contract drift.
- **Endpoints**: `gui/preview/mock-api.ts` answers snapshot, events (one
  frame, held open), configuration, release, and telemetry. Every other
  endpoint returns 501 `preview.unsupported`, so actions, inspection,
  discovery, and source edits show their error state. The preview is for
  presentation only. Use loop 3 for behavior.
- **Theme** comes from `localStorage['vitepress-theme-appearance']`.
  `shoot.ts --theme` sets it for you.
- **Port**: `Port 5199 is already in use` means another server holds the
  port, often a leftover preview or a throwaway mock from an earlier
  session. Check with `lsof -nP -iTCP:5199 -sTCP:LISTEN`. `shoot.ts` is
  unaffected because it picks its own port.

### 2. Prove it: Vitest (seconds)

```sh
cd gui && ./node_modules/.bin/vue-tsc --noEmit && ./node_modules/.bin/vitest run
```

Component tests mount `WorkspaceView` with `fakeClient()` from
`src/test-support.ts` and push snapshot events. `rowNames(wrapper)` reads
the rendered names. Write new presentation assertions here. The happy-dom
`AbortError` stack traces at teardown are noise, not failures. Call the
binaries in `./node_modules/.bin` directly, because `pnpm run` refuses to
start when `node_modules` is stale.

### 3. Exercise the real core: Playwright + Go harness (minutes)

For behavior that crosses the local API: mutations, confirmations, sync,
sources, first run, discovery, inspection.

```sh
cd gui && ./node_modules/.bin/vite build \
  && ./node_modules/.bin/playwright test tests/sources.spec.ts -g "user directory source"
```

- The specs serve the **built** `dist`, so rebuild after every `src/`
  edit. A stale `dist` passes or fails against old code.
- Each spec starts a real local API with `go test ./internal/localapi -run
  '^TestLocalAPI(User)?Harness$'`. The fixture comes from
  `AM_LOCALAPI_USER_FIXTURE`; the setups are in
  `internal/localapi/harness_test.go`. The harness writes its bootstrap
  JSON to fd 3 and exits after 2 minutes, so it can't serve as a
  long-lived dev backend. Harness stderr is kept in
  `gui/test-results/harness-stderr-<pid>.log`.
- Run one spec or `-g` title while you iterate. The full suite runs
  serially (`workers: 1`) with a 120 s test timeout. Running heavy work
  alongside it (screenshot loops, Go suites) causes spurious timeouts, so
  rerun a timed-out test alone before you treat it as a regression.
- Option and button accessible names come from rendered text (`tabTo` in
  `tests/keyboard.ts`, `getByRole('option', { name })`). If you change row
  text, grep `gui/tests/` and `gui/src/*.test.ts` for the old string
  first, then update every match in one pass.

### Walk a flow for debugging

To reproduce a bug or watch a flow against the real core, write a
throwaway **walk** in `gui/tests/walk/` (gitignored) and run it with its own
config. `playwright.config.ts` lists spec files explicitly, and the CLI
path argument only filters that list, so a new file under `gui/tests/` is
silently skipped:

```sh
cd gui && ./node_modules/.bin/vite build \
  && ./node_modules/.bin/playwright test -c playwright.walk.config.ts
```

Start from this template:

```ts
import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { distIndexExists, launchBuiltUserApp } from '../app'
import { startHarness } from '../harness'

test('walk: open sources', async ({ page }, testInfo) => {
  expect(distIndexExists(), 'run vite build in gui/ first').toBe(true)
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'sources' })
  const { bootstrap } = harness
  const step = async (name: string) => {
    const path = testInfo.outputPath(`${name}.png`)
    await page.screenshot({ path, animations: 'disabled' })
    console.log(name, path)
  }
  try {
    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible({ timeout: 60_000 })
    await step('01-loaded')
    await page.getByRole('button', { name: 'Sources' }).click()
    await expect(page.getByRole('dialog', { name: 'Sources' })).toBeVisible()
    await step('02-sources-open')
    console.log(await readFile(join(bootstrap.home!, '.agents', 'config.yaml'), 'utf8'))
  } finally {
    await harness.stop()
  }
})
```

- **Step discipline**: after every input, assert the visible result before
  the next input, then capture with `step()`. Print the config, lock, or
  materialized file the step should have changed. The bootstrap carries
  `home`, `repo`, the XDG roots, and fixture-specific paths. A failed
  assertion then names the exact step where the flow diverged, and the
  printed PNG paths are the evidence.
- **Time budget**: the harness exits 2 minutes after bootstrap, and the
  test timeout is 120 s. Keep a walk to one flow, and split longer
  journeys into separate walks.
- **Locate by role and name** (`getByRole('button', { name })`). Those are
  the same accessible names the real specs use, so a walk that works can
  be promoted unchanged.
- **Output**: PNGs land under `gui/test-results/walk/<test>/`. `tsconfig.json`
  includes `tests/**`, so delete a walk when you're done, because a stale
  one with a type error fails local `vue-tsc`.
- **Promote** a walk that reproduced a real bug: move it to `gui/tests/`,
  add it to `testMatch`, and bind its `// spec:` scenario.

Fixtures (`AM_LOCALAPI_USER_FIXTURE`; setups in
`internal/localapi/harness_test.go`; the spec in parentheses shows how
each one is driven):

| Fixture | State |
|---|---|
| *(unset)* | v4 user root, one directory source with skill `solo`, devin target. The repo's `.agents/config.yaml` and `config.local.yaml` are deliberately malformed (the user-scope GUI must ignore them), and the org config path doesn't exist |
| `sources` | Root plus an included fragment source, an org directory source, and an extra directory you can add (`addSourceDir`) (`sources.spec.ts`) |
| `sources-git` | Same, with a local bare Git source to add (`gitSourceURL`) and harness overrides |
| `marketplace` | `sources` plus a stub Marketplace endpoint (`marketplaceEndpoint`) |
| `harnesses` | User targets devin plus an unknown future harness, and an org layer targeting claude (`gui.spec.ts`, `refresh.spec.ts`) |
| `first-run` | No user root; a detected `.factory` harness, an optional directory, and a Marketplace stub whose first call fails (`gui.spec.ts`) |
| `legacy` | A v2 user root that offers migration (`confirmations.spec.ts`) |
| `navigation` | 60 skills, for scrolling, search, and keyboard paths (`navigation.spec.ts`) |
| `held-sync` | Sync blocks until the test creates the `syncRelease` file (`confirmations.spec.ts`) |
| `release` | Empty org layer; pair it with `AM_LOCALAPI_RELEASE_*` (`releases.spec.ts`) |
| `work35-discovery` | Marketplace discovery with acquisition and search barriers (`discovery.spec.ts`) |
| `work36-inspection` | Verified Marketplace inspection with support, image, and unsafe files (`inspection.spec.ts`) |
| `work37-installed`, `work37-installed-missing` | An installed skill with support files, optionally with one deleted (`installed-skill-files.spec.ts`) |
| `work39-resource-actions` | Marketplace install/remove; `AM_LOCALAPI_USER_INITIAL_MODE` = `available`, `user-installed`, or `org-optional` (`resource-actions.spec.ts`) |
| `work40-resource-actions` | Directory-registry install/remove; mode `root-owned`, `source-included`, or `selection-included` (`registry-resource-actions.spec.ts`) |

When you need a state no fixture provides, add a branch to
`TestLocalAPIUserHarness`, named for the state rather than the work item.

### 4. Native host (only for window, binding, or lifecycle changes)

```sh
CGO_ENABLED=1 go test ./cmd/agent-marketplace-gui -run '^TestNativeAppOptionsWiring$' -count=1
make gui-build && ./agent-marketplace-gui
```

`make gui-build` builds the frontend and then the Wails binary. The built
app runs against **your real HOME and organization config**, and its
actions really sync. Use it to check native chrome (title bar, close
handling, single instance), never to review fixture states.

## Gotchas

- Nuxt UI's `soft` + `neutral` button background is hard-wired to
  `bg-elevated`, which equals the canvas, so it renders with no fill.
  Restyle through `ui-options.ts` or a scoped class in `style.css`. Changing
  `--ui-bg-elevated` also changes popovers and menus.
- `vite.config.ts` imports `./ui-options` without an extension, so Vite
  prints a `configLoader: 'native'` warning on every preview run. The
  warning is harmless.
