import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { compareVersions, parseVersion } from '../src/shared/docs-versions.mjs'

function fail(code, message) {
  throw Object.assign(new Error(message), { code })
}

function sanitizedGitEnvironment() {
  return Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GIT_CONFIG_')))
}

function safePath(path) {
  if (!path || isAbsolute(path) || path.includes('\\') || path.split('/').some((part) => !part || part === '.' || part === '..')) {
    fail('docs.invalid-path', `Unsafe documentation path: ${path}`)
  }
  return path
}

function assertDirectory(path) {
  if (existsSync(path)) {
    if (!lstatSync(path).isDirectory()) fail('docs.unsafe-file', `Expected a directory, not a symlink or file: ${path}`)
  } else {
    assertDirectory(dirname(path))
    mkdirSync(path)
  }
}

function readTree(root) {
  if (!lstatSync(root).isDirectory()) fail('docs.unsafe-file', `Unsafe documentation directory: ${root}`)
  const files = {}
  const walk = (directory, prefix = '') => {
    for (const entry of readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const name = safePath(`${prefix}${entry.name}`)
      const path = join(directory, entry.name)
      if (entry.isDirectory()) walk(path, `${name}/`)
      else if (entry.isFile()) files[name] = readFileSync(path)
      else fail('docs.unsafe-file', `Documentation must contain only regular files: ${path}`)
    }
  }
  walk(root)
  return files
}

function writeTree(root, files) {
  for (const [name, content] of Object.entries(files)) {
    const path = join(root, safePath(name))
    assertDirectory(dirname(path))
    writeFileSync(path, content, { flag: 'wx' })
  }
}

function hashes(files) {
  return Object.fromEntries(Object.keys(files).sort().map((name) => [name, createHash('sha256').update(files[name]).digest('hex')]))
}

function validateDocs(files, sidebar) {
  Object.keys(files).forEach(safePath)
  if (!files['index.md']) fail('docs.missing-overview', 'Documentation must contain index.md')
  for (const name of Object.keys(files)) {
    if (/^(preview|\d+\.\d+\.\d+(?:-rc\.?\d+)?)(\/|\.md$)/.test(name)) {
      fail('docs.invalid-path', `Reserved documentation route: ${name}`)
    }
  }
  const visit = (items) => {
    if (!Array.isArray(items)) fail('docs.invalid-sidebar', 'Sidebar must be an array')
    for (const item of items) {
      if (typeof item.text !== 'string') fail('docs.invalid-sidebar', 'Sidebar entries need text')
      if (item.link !== undefined) {
        if (typeof item.link !== 'string' || !item.link.startsWith('/docs/')) fail('docs.invalid-sidebar', `Invalid sidebar link: ${item.link}`)
        const page = item.link.slice('/docs/'.length).split('#')[0].replace(/\.(md|html)$/, '')
        if (!files[`${page || 'index'}.md`]) fail('docs.invalid-sidebar', `Missing sidebar page: ${item.link}`)
      }
      if (item.items) visit(item.items)
    }
  }
  visit(sidebar)
}

export function createSnapshot(site, version, files, sidebar, source) {
  parseVersion(version)
  validateDocs(files, sidebar)
  const target = join(site, 'versions', version)
  if (existsSync(target)) fail('docs.snapshot-exists', `Snapshot ${version} already exists; releases cannot be overwritten`)
  const contents = Object.fromEntries(Object.entries(files).map(([name, bytes]) => [`docs/${name}`, bytes]))
  contents['sidebar.json'] = Buffer.from(`${JSON.stringify(sidebar, null, 2)}\n`)
  const manifest = { version, source, files: hashes(contents) }
  assertDirectory(dirname(target))
  mkdirSync(target)
  try {
    writeTree(target, { ...contents, 'manifest.json': Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`) })
  } catch (error) {
    rmSync(target, { recursive: true, force: true })
    throw error
  }
  return manifest
}

export function snapshotFromGit(site, version, ref, sidebar) {
  parseVersion(version)
  const env = sanitizedGitEnvironment()
  const git = (...args) => execFileSync('git', ['-C', site, ...args], { env, maxBuffer: 32 * 1024 * 1024 })
  const commit = git('rev-parse', '--verify', '--end-of-options', `${ref}^{commit}`).toString().trim()
  const prefix = git('rev-parse', '--show-prefix').toString().trim()
  const docsPath = `${prefix}docs`
  const tree = git('ls-tree', '-rz', commit, '--', 'docs').toString().split('\0').filter(Boolean)
  const files = {}
  for (const entry of tree) {
    const [metadata, path] = entry.split('\t')
    const [mode, type, object] = metadata.split(' ')
    if (type !== 'blob' || !['100644', '100755'].includes(mode)) fail('docs.unsafe-file', `Unsafe Git documentation entry: ${path}`)
    files[safePath(path.slice('docs/'.length))] = git('cat-file', 'blob', object)
  }
  return createSnapshot(site, version, files, sidebar, { commit, path: docsPath })
}

export function assertAppendOnly(site, ref) {
  const env = sanitizedGitEnvironment()
  const git = (...args) => execFileSync('git', ['-C', site, ...args], { encoding: 'utf8', env })
  const commit = git('rev-parse', '--verify', '--end-of-options', `${ref}^{commit}`).trim()
  const priorPaths = git('ls-tree', '-rz', '--name-only', commit, '--', 'versions').split('\0').filter(Boolean)
  const priorVersions = new Set(priorPaths.map((path) => path.split('/')[1]))
  const prefix = git('rev-parse', '--show-prefix').trim()
  const changed = git('diff', '--no-relative', '--no-renames', '--name-only', '-z', commit, '--', 'versions').split('\0').filter(Boolean)
  for (const path of changed) {
    if (priorVersions.has(path.slice(prefix.length).split('/')[1])) {
      fail('docs.snapshot-rewritten', `Published documentation is immutable: ${path}`)
    }
  }
}

export function loadReleases(site) {
  const root = join(site, 'versions')
  if (!existsSync(root)) fail('docs.no-releases', 'Create a documentation snapshot before building the site')
  if (!lstatSync(root).isDirectory()) fail('docs.unsafe-file', `Unsafe snapshot root: ${root}`)
  const versions = readdirSync(root).sort(compareVersions)
  if (!versions.length) fail('docs.no-releases', 'No documentation snapshots found')
  return versions.map((version) => {
    parseVersion(version)
    const contents = readTree(join(root, version))
    const manifest = JSON.parse(contents['manifest.json'] ?? 'null')
    delete contents['manifest.json']
    if (manifest?.version !== version || JSON.stringify(manifest.files) !== JSON.stringify(hashes(contents))) {
      fail('docs.snapshot-integrity', `Snapshot ${version} differs from its manifest`)
    }
    const sidebar = JSON.parse(contents['sidebar.json'])
    const files = Object.fromEntries(Object.entries(contents).filter(([name]) => name.startsWith('docs/')).map(([name, bytes]) => [name.slice(5), bytes]))
    validateDocs(files, sidebar)
    return { version, source: manifest.source, files, sidebar }
  })
}

export function verifySnapshots(site, tag, sidebar) {
  const releases = loadReleases(site)
  if (tag) {
    const expected = tag.replace(/^v/, '')
    parseVersion(expected)
    const release = releases.find(({ version }) => version === expected)
    if (!release) fail('docs.missing-release', `Run pnpm docs:snapshot ${expected} before tagging ${tag}`)
    if (JSON.stringify(hashes(release.files)) !== JSON.stringify(hashes(readTree(join(site, 'docs')))) || JSON.stringify(release.sidebar) !== JSON.stringify(sidebar)) {
      fail('docs.release-drift', `Snapshot ${expected} does not match the tagged documentation and sidebar`)
    }
  }
  return releases.length
}

export function prepareSite(site, previewSidebar) {
  const releases = loadReleases(site)
  const previewFiles = readTree(join(site, 'docs'))
  validateDocs(previewFiles, previewSidebar)
  const latest = releases[0]
  const sources = [
    { ...latest, version: 'latest', release: latest.version, path: '/docs/', label: `Latest (${latest.version})` },
    { version: 'preview', files: previewFiles, sidebar: previewSidebar, path: '/docs/preview/', label: 'Preview' },
    ...releases.map((release) => ({ ...release, path: `/docs/${release.version}/`, label: release.version }))
  ]
  const srcDir = join(site, '.vitepress/cache/content')
  for (const path of ['.vitepress', '.vitepress/cache', '.vitepress/cache/content']) assertDirectory(join(site, path))
  rmSync(srcDir, { recursive: true })
  const output = { 'index.md': readFileSync(join(site, 'index.md')) }
  const sidebar = {}
  const versions = []
  const routeSidebar = (items, path) => items.map((item) => ({
    ...item,
    ...(item.link && { link: `${path}${item.link.slice('/docs/'.length)}` }),
    ...(item.items && { items: routeSidebar(item.items, path) })
  }))
  for (const source of sources) {
    for (const [name, bytes] of Object.entries(source.files)) output[`${source.path.slice(1)}${name}`] = bytes
    sidebar[source.path] = routeSidebar(source.sidebar, source.path)
    versions.push({
      version: source.version, release: source.release, path: source.path, label: source.label,
      source: source.source,
      pages: Object.keys(source.files).filter((name) => name.endsWith('.md')).map((name) => name.slice(0, -3)).sort()
    })
  }
  writeTree(srcDir, output)
  return { srcDir, sidebar, versions }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const site = resolve(dirname(fileURLToPath(import.meta.url)), '..')
  const [command, version, ref = 'HEAD'] = process.argv.slice(2)
  try {
    const { default: sidebar } = await import('../.vitepress/sidebar.mjs')
    if (command === 'snapshot') {
      const manifest = snapshotFromGit(site, version, ref, sidebar)
      console.log(`Created immutable docs snapshot ${manifest.version} from ${manifest.source.commit}`)
    } else if (command === 'check-history' && version) {
      loadReleases(site)
      assertAppendOnly(site, version)
      console.log(`Published documentation is unchanged from ${version}`)
    } else if (command === 'check') {
      console.log(`Verified ${verifySnapshots(site, version, sidebar)} immutable documentation snapshot(s)`)
    } else {
      fail('docs.usage', 'Usage: node scripts/docs.mjs snapshot <version> [git-ref] | check [release-tag] | check-history <base-ref>')
    }
  } catch (error) {
    console.error(`${error.code ?? 'docs.failed'}: ${error.message}`)
    process.exitCode = 1
  }
}
