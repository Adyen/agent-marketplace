import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { stripVTControlCharacters } from 'node:util'
import { test } from 'node:test'

const source = fileURLToPath(new URL('../', import.meta.url))

test('development server reloads Preview without changing released docs', { timeout: 30000 }, async (t) => {
  const root = mkdtempSync(join(tmpdir(), 'docs-dev-'))
  const site = join(root, 'site')
  mkdirSync(site)
  for (const path of ['.vitepress/config.mts', '.vitepress/sidebar.mjs', '.vitepress/theme', 'docs', 'versions', 'scripts', 'src', 'index.md', 'package.json']) {
    cpSync(join(source, path), join(site, path), { recursive: true })
  }
  cpSync(join(source, '../design'), join(root, 'design'), { recursive: true })
  symlinkSync(join(source, 'node_modules'), join(site, 'node_modules'), 'dir')
  const child = spawn(process.execPath, [join(source, 'node_modules/vitepress/bin/vitepress.js'), 'dev', site, '--host', '127.0.0.1', '--port', '0'], {
    cwd: site,
    env: { ...process.env, SITE_BASE: '/', FORCE_COLOR: '0' },
    stdio: ['ignore', 'pipe', 'pipe']
  })
  const exited = once(child, 'exit')
  let output = ''
  child.stdout.on('data', (chunk) => { output += stripVTControlCharacters(chunk.toString()) })
  child.stderr.on('data', (chunk) => { output += stripVTControlCharacters(chunk.toString()) })
  t.after(async () => {
    child.kill('SIGTERM')
    await exited
    rmSync(root, { recursive: true, force: true })
  })
  const waitFor = async (condition) => {
    const deadline = Date.now() + 10000
    while (!condition() && Date.now() < deadline && child.exitCode === null) await delay(100)
    assert.ok(condition(), output)
  }
  await waitFor(() => /http:\/\/127\.0\.0\.1:\d+/.test(output))
  const path = 'how-to/author/add-a-resource-to-a-registry.md'
  const generated = join(site, '.vitepress/cache/content/docs')
  const released = readFileSync(join(generated, path))
  writeFileSync(join(site, 'docs', path), '# Preview reload sentinel\n')
  await waitFor(() => existsSync(join(generated, 'preview', path)) && readFileSync(join(generated, 'preview', path), 'utf8') === '# Preview reload sentinel\n')
  assert.deepEqual(readFileSync(join(generated, path)), released)
  await waitFor(() => (output.match(/http:\/\/127\.0\.0\.1:\d+/g) ?? []).length >= 2)
  const url = output.match(/http:\/\/127\.0\.0\.1:\d+/g).at(-1)
  const response = await fetch(`${url}/docs/preview/${path}?import`)
  assert.equal(response.status, 200)
  assert.match(await response.text(), /Preview reload sentinel/)
})
