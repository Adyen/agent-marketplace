import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { assertAppendOnly, createSnapshot, loadReleases, prepareSite, snapshotFromGit, verifySnapshots } from '../scripts/docs.mjs'
import { compareVersions, docsLocation, searchVersionFilter, switchVersion } from '../src/shared/docs-versions.mjs'

const sidebar = [{ text: 'Overview', link: '/docs/' }, { text: 'Guide', link: '/docs/guide' }]
const files = {
  'index.md': Buffer.from('# Release overview\n\n[Guide](./guide.md)\n'),
  'guide.md': Buffer.from('# Released guide\n\n![Image](./images/example.png)\n'),
  'images/example.png': Buffer.from([137, 80, 78, 71])
}

function workspace(t) {
  const root = mkdtempSync(join(tmpdir(), 'docs-versions-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  mkdirSync(join(root, 'docs'))
  writeFileSync(join(root, 'index.md'), '# Landing\n')
  writeFileSync(join(root, 'docs/index.md'), '# Preview overview\n')
  writeFileSync(join(root, 'docs/guide.md'), '# Unreleased guide\n')
  return root
}

function snapshot(root, version = '3.2.0-rc.13') {
  createSnapshot(root, version, files, sidebar, { commit: 'a'.repeat(40), path: 'site/docs' })
}

function commitFixture(root) {
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GIT_CONFIG_')))
  env.GIT_CONFIG_GLOBAL = '/dev/null'
  env.GIT_CONFIG_NOSYSTEM = '1'
  const git = (...args) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8', env })
  git('init', '-q')
  git('add', '.')
  git('-c', 'user.name=Docs test', '-c', 'user.email=docs@example.invalid', '-c', 'commit.gpgsign=false', 'commit', '-qm', 'Fixture')
  return git('rev-parse', 'HEAD').trim()
}

test('stable and RC releases sort semantically, not lexically', () => {
  assert.deepEqual(['3.2.0-rc.9', '3.1.1', '3.2.0', '3.2.0-rc.13'].sort(compareVersions),
    ['3.2.0', '3.2.0-rc.13', '3.2.0-rc.9', '3.1.1'])
})

test('snapshots preserve bytes and navigation and refuse replacement', (t) => {
  const root = workspace(t)
  snapshot(root)
  const before = readFileSync(join(root, 'versions/3.2.0-rc.13/manifest.json'))
  assert.deepEqual(readFileSync(join(root, 'versions/3.2.0-rc.13/docs/images/example.png')), files['images/example.png'])
  assert.deepEqual(JSON.parse(readFileSync(join(root, 'versions/3.2.0-rc.13/sidebar.json'))), sidebar)
  assert.throws(() => snapshot(root), { code: 'docs.snapshot-exists' })
  assert.deepEqual(readFileSync(join(root, 'versions/3.2.0-rc.13/manifest.json')), before)
  assert.deepEqual(loadReleases(root).map(({ version }) => version), ['3.2.0-rc.13'])
})

test('invalid versions and escaping paths fail before any snapshot is written', (t) => {
  const root = workspace(t)
  for (const version of ['../outside', 'preview', '3.02.0', 'v3.2.0', '3.2.0-beta.1']) {
    assert.throws(() => snapshot(root, version), { code: 'docs.invalid-version' })
  }
  for (const name of ['../outside.md', '/absolute.md', 'a/../../escape', 'a\\escape']) {
    assert.throws(() => createSnapshot(root, '3.2.0', { ...files, [name]: Buffer.from('x') }, sidebar, {}), { code: 'docs.invalid-path' })
  }
  assert.deepEqual(readdirSync(root).sort(), ['docs', 'index.md'])
})

test('snapshot validation detects changed, missing and unexpected content', (t) => {
  const root = workspace(t)
  snapshot(root)
  const guide = join(root, 'versions/3.2.0-rc.13/docs/guide.md')
  writeFileSync(guide, '# Changed\n')
  assert.throws(() => loadReleases(root), { code: 'docs.snapshot-integrity' })
  writeFileSync(guide, files['guide.md'])
  writeFileSync(join(root, 'versions/3.2.0-rc.13/docs/extra.md'), '# Extra\n')
  assert.throws(() => loadReleases(root), { code: 'docs.snapshot-integrity' })
  rmSync(join(root, 'versions/3.2.0-rc.13/docs/extra.md'))
  rmSync(guide)
  assert.throws(() => loadReleases(root), { code: 'docs.snapshot-integrity' })
})

test('source and snapshot symlinks are rejected without following them', (t) => {
  const root = workspace(t)
  snapshot(root)
  symlinkSync(join(root, 'index.md'), join(root, 'docs/escape.md'))
  assert.throws(() => prepareSite(root, sidebar), { code: 'docs.unsafe-file' })
  rmSync(join(root, 'docs/escape.md'))
  symlinkSync(join(root, 'index.md'), join(root, 'versions/3.2.0-rc.13/docs/escape.md'))
  assert.throws(() => loadReleases(root), { code: 'docs.unsafe-file' })
})

test('build preparation refuses a symlinked cache without deleting external files', (t) => {
  const root = workspace(t)
  snapshot(root)
  mkdirSync(join(root, '.vitepress'))
  mkdirSync(join(root, 'outside/content'), { recursive: true })
  writeFileSync(join(root, 'outside/content/keep'), 'User content')
  symlinkSync(join(root, 'outside'), join(root, '.vitepress/cache'))
  assert.throws(() => prepareSite(root, sidebar), { code: 'docs.unsafe-file' })
  assert.equal(readFileSync(join(root, 'outside/content/keep'), 'utf8'), 'User content')
})

test('generated routes separate latest, archived, and preview bytes and sidebar', (t) => {
  const root = workspace(t)
  snapshot(root)
  const previewSidebar = [...sidebar, { text: 'New page', link: '/docs/new' }]
  writeFileSync(join(root, 'docs/new.md'), '# New preview page\n')
  const result = prepareSite(root, previewSidebar)
  assert.equal(readFileSync(join(result.srcDir, 'docs/guide.md'), 'utf8'), files['guide.md'].toString())
  assert.equal(readFileSync(join(result.srcDir, 'docs/3.2.0-rc.13/guide.md'), 'utf8'), files['guide.md'].toString())
  assert.equal(readFileSync(join(result.srcDir, 'docs/preview/guide.md'), 'utf8'), '# Unreleased guide\n')
  assert.deepEqual(result.sidebar['/docs/3.2.0-rc.13/'], [
    { text: 'Overview', link: '/docs/3.2.0-rc.13/' },
    { text: 'Guide', link: '/docs/3.2.0-rc.13/guide' }
  ])
  assert.equal(result.sidebar['/docs/'].length, 2)
  assert.equal(result.sidebar['/docs/preview/'].length, 3)
  assert.equal(result.versions[0].version, 'latest')
  assert.equal(result.versions[0].release, '3.2.0-rc.13')
  assert.deepEqual(readFileSync(join(result.srcDir, 'docs/3.2.0-rc.13/images/example.png')), files['images/example.png'])
  assert.equal(readFileSync(join(root, 'docs/guide.md'), 'utf8'), '# Unreleased guide\n')
})

test('adding a release advances latest without modifying older releases', (t) => {
  const root = workspace(t)
  snapshot(root)
  createSnapshot(root, '3.2.0', { ...files, 'guide.md': Buffer.from('# Stable guide\n') }, sidebar, {})
  const result = prepareSite(root, sidebar)
  assert.equal(readFileSync(join(result.srcDir, 'docs/guide.md'), 'utf8'), '# Stable guide\n')
  assert.equal(readFileSync(join(result.srcDir, 'docs/3.2.0-rc.13/guide.md'), 'utf8'), files['guide.md'].toString())
})

test('invalid navigation and missing overview fail rather than publishing broken snapshots', (t) => {
  const root = workspace(t)
  assert.throws(() => createSnapshot(root, '3.2.0', files, [{ text: 'Missing', link: '/docs/missing' }], {}), { code: 'docs.invalid-sidebar' })
  assert.throws(() => createSnapshot(root, '3.2.0', { 'guide.md': files['guide.md'] }, sidebar, {}), { code: 'docs.missing-overview' })
})

test('snapshot CLI reads a pinned Git tree rather than dirty working docs', (t) => {
  const root = workspace(t)
  const commit = commitFixture(root)
  writeFileSync(join(root, 'docs/guide.md'), '# Dirty working copy\n')
  snapshotFromGit(root, '3.2.0', 'HEAD', sidebar)
  const release = loadReleases(root)[0]
  assert.equal(release.source.commit, commit)
  assert.equal(readFileSync(join(root, 'versions/3.2.0/docs/guide.md'), 'utf8'), '# Unreleased guide\n')
})

test('Git fixtures ignore ambient configuration injection variables', (t) => {
  const root = workspace(t)
  const keys = ['GIT_CONFIG_COUNT', 'GIT_CONFIG_KEY_0', 'GIT_CONFIG_VALUE_0']
  const previous = Object.fromEntries(keys.map((key) => [key, process.env[key]]))
  process.env.GIT_CONFIG_COUNT = '1'
  process.env.GIT_CONFIG_KEY_0 = 'core.bare'
  process.env.GIT_CONFIG_VALUE_0 = 'not-a-boolean'
  try {
    assert.match(commitFixture(root), /^[0-9a-f]{40}$/)
  } finally {
    for (const key of keys) {
      if (previous[key] === undefined) delete process.env[key]
      else process.env[key] = previous[key]
    }
  }
})

test('snapshot Git commands ignore ambient Git configuration', (t) => {
  const root = workspace(t)
  const commit = commitFixture(root)
  const config = join(root, 'invalid.gitconfig')
  writeFileSync(config, '[broken\n')
  const previous = process.env.GIT_CONFIG_GLOBAL
  process.env.GIT_CONFIG_GLOBAL = config
  try {
    snapshotFromGit(root, '3.2.0', 'HEAD', sidebar)
  } finally {
    if (previous === undefined) delete process.env.GIT_CONFIG_GLOBAL
    else process.env.GIT_CONFIG_GLOBAL = previous
  }
  assert.equal(loadReleases(root)[0].source.commit, commit)
})

test('history Git commands ignore ambient Git configuration', (t) => {
  const root = workspace(t)
  snapshot(root)
  const baseline = commitFixture(root)
  const config = join(root, 'invalid.gitconfig')
  writeFileSync(config, '[broken\n')
  const previous = process.env.GIT_CONFIG_GLOBAL
  process.env.GIT_CONFIG_GLOBAL = config
  try {
    assertAppendOnly(root, baseline)
  } finally {
    if (previous === undefined) delete process.env.GIT_CONFIG_GLOBAL
    else process.env.GIT_CONFIG_GLOBAL = previous
  }
})

test('release checks reject missing snapshots and content or navigation drift', (t) => {
  const root = workspace(t)
  commitFixture(root)
  snapshotFromGit(root, '3.2.0', 'HEAD', sidebar)
  assert.equal(verifySnapshots(root, 'v3.2.0', sidebar), 1)
  assert.throws(() => verifySnapshots(root, 'v3.3.0', sidebar), { code: 'docs.missing-release' })
  assert.throws(() => verifySnapshots(root, 'v3.2.0', [...sidebar].reverse()), { code: 'docs.release-drift' })
  writeFileSync(join(root, 'docs/guide.md'), '# Changed after snapshot\n')
  assert.throws(() => verifySnapshots(root, 'v3.2.0', sidebar), { code: 'docs.release-drift' })
  assert.equal(verifySnapshots(root, undefined, sidebar), 1)
})

test('history check permits new versions but rejects replacing an existing snapshot and its manifest', (t) => {
  const root = workspace(t)
  snapshot(root)
  const baseline = commitFixture(root)
  snapshot(root, '3.2.0')
  assertAppendOnly(root, baseline)
  rmSync(join(root, 'versions/3.2.0-rc.13'), { recursive: true })
  createSnapshot(root, '3.2.0-rc.13', { ...files, 'guide.md': Buffer.from('# Rewritten history\n') }, sidebar, {})
  assert.equal(loadReleases(root).length, 2)
  assert.throws(() => assertAppendOnly(root, baseline), { code: 'docs.snapshot-rewritten' })
})

test('history comparison also protects a site nested inside a repository', (t) => {
  const root = workspace(t)
  const site = join(root, 'site')
  mkdirSync(site)
  snapshot(site)
  const baseline = commitFixture(root)
  assertAppendOnly(site, baseline)
  writeFileSync(join(site, 'versions/3.2.0-rc.13/docs/guide.md'), '# Changed\n')
  assert.throws(() => assertAppendOnly(site, baseline), { code: 'docs.snapshot-rewritten' })
})

test('version switching preserves existing pages, handles base paths, and falls back to overview', () => {
  const versions = [
    { version: 'latest', path: '/docs/', pages: ['index', 'guide'] },
    { version: 'preview', path: '/docs/preview/', pages: ['index', 'guide', 'new'] },
    { version: '3.2.0-rc.13', path: '/docs/3.2.0-rc.13/', pages: ['index', 'guide'] }
  ]
  assert.deepEqual(docsLocation('/project/docs/preview/guide', versions, '/project/'), { version: 'preview', page: 'guide' })
  assert.equal(switchVersion('/project/docs/preview/guide', '3.2.0-rc.13', versions, '/project/'), '/project/docs/3.2.0-rc.13/guide')
  assert.equal(switchVersion('/docs/preview/new', 'latest', versions), '/docs/')
  assert.equal(switchVersion('/docs/3.2.0-rc.13/index.html', 'preview', versions), '/docs/preview/')
  assert.equal(switchVersion('/docs/preview/guide.html#old-anchor', 'latest', versions), '/docs/guide')
  assert.equal(docsLocation('/marketplace/', versions), null)
})

test('serialized search filter excludes other versions at root and prefixed deployments', (t) => {
  const originalWindow = globalThis.window
  t.after(() => { globalThis.window = originalWindow })
  const filter = new Function(`return (${searchVersionFilter.toString()})`)()
  for (const base of ['/', '/project/', '/docs/']) {
    globalThis.window = { location: { pathname: `${base}docs/preview/guide` }, __VP_SITE_DATA__: { base } }
    assert.equal(filter({ id: `${base}docs/preview/guide#section` }), true)
    assert.equal(filter({ id: `${base}docs/guide#section` }), false)
    assert.equal(filter({ id: `${base}docs/3.2.0-rc.13/guide#section` }), false)
    window.location.pathname = `${base}docs/preview`
    assert.equal(filter({ id: `${base}docs/preview/guide#section` }), true)
    assert.equal(filter({ id: `${base}docs/guide#section` }), false)
    window.location.pathname = `${base}docs/3.2.0-rc.13/guide`
    assert.equal(filter({ id: `${base}docs/3.2.0-rc.13/guide#section` }), true)
    assert.equal(filter({ id: `${base}docs/3.2.0-rc.9/guide#section` }), false)
    window.location.pathname = base
    assert.equal(filter({ id: `${base}docs/guide#section` }), true)
    assert.equal(filter({ id: `${base}docs/preview/guide#section` }), false)
  }
})
