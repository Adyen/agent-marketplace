import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readdir, readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const repository = resolve(fileURLToPath(new URL('../../', import.meta.url)))
const output = resolve(repository, 'site/public/landing')
const expectedFixtureDigest = 'b89fe775d69e2a49b7ce805e76daadff346700f5dcfc4e61f9fb69d1d87ef507'

async function captureSnapshot() {
  execFileSync('make', ['site-screenshots'], { cwd: repository, stdio: 'inherit' })
  const manifestBytes = await readFile(resolve(output, 'manifest.json'))
  const manifest = JSON.parse(manifestBytes.toString())
  assert.equal(manifest.fixture.file, 'web/tests/e2e/fixtures/landing.json')
  assert.equal(manifest.fixture.sha256, expectedFixtureDigest)
  const imageHashes = await Promise.all(manifest.images.map(async image => {
    const imageBytes = await readFile(resolve(output, image.file))
    assert.equal(createHash('sha256').update(imageBytes).digest('hex'), image.sha256, image.file)
    return [image.file, image.sha256]
  }))
  assert.deepEqual(
    (await readdir(output)).filter(file => file.endsWith('.webp')).sort(),
    manifest.images.map(image => image.file).sort(),
  )
  return { manifestBytes, imageHashes }
}

// spec: marketplace-web-ui/regenerated-depictions-are-reproducible
test('capture command reproduces the fixture manifest and image hashes', async () => {
  const fixtureBytes = await readFile(resolve(repository, 'web/tests/e2e/fixtures/landing.json'))
  assert.equal(createHash('sha256').update(fixtureBytes).digest('hex'), expectedFixtureDigest)

  const first = await captureSnapshot()
  const second = await captureSnapshot()
  assert.deepEqual(second.manifestBytes, first.manifestBytes)
  assert.deepEqual(second.imageHashes, first.imageHashes)
  assert.deepEqual(
    first.imageHashes.map(([file]) => file).sort(),
    [
      'catalog-desktop-dark.webp',
      'catalog-desktop-light.webp',
      'catalog-narrow-dark.webp',
      'catalog-narrow-light.webp',
      'detail-desktop-dark.webp',
      'detail-desktop-light.webp',
      'detail-narrow-dark.webp',
      'detail-narrow-light.webp',
    ],
  )
})
