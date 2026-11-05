import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { expect, test } from '@playwright/test'

const appURL = `http://127.0.0.1:${process.env.E2E_APP_PORT || '4321'}`
const fixturePath = resolve(process.cwd(), process.env.E2E_LANDING_FIXTURE_PATH || 'tests/e2e/fixtures/landing.json')
const outputDirectory = resolve(process.cwd(), '../site/public/landing')
const screens = [
  { name: 'desktop', viewport: { width: 1440, height: 960 } },
  { name: 'narrow', viewport: { width: 390, height: 844 } },
] as const
const appearances = ['light', 'dark'] as const

function digest(bytes: Buffer) {
  return createHash('sha256').update(bytes).digest('hex')
}

// spec: marketplace-web-ui/browser-frame-shows-catalog-and-resource-detail
test('captures the fixture catalog and clean resource detail in both themes and widths', async ({ browser }) => {
  test.setTimeout(120_000)
  const cwebp = process.env.CWEBP_BIN
  if (!cwebp || !existsSync(cwebp)) {
    throw new Error('cwebp is required for landing captures; set CWEBP_BIN to an installed WebP encoder.')
  }
  const ffmpeg = process.env.FFMPEG_BIN
  if (!ffmpeg || !existsSync(ffmpeg)) {
    throw new Error('ffmpeg is required to normalize landing screenshots for cwebp; set FFMPEG_BIN to an installed ffmpeg.')
  }

  const fixtureBytes = await readFile(fixturePath)
  const fixture = JSON.parse(fixtureBytes.toString()) as { items: unknown[] }
  expect(fixture.items).toHaveLength(5)
  await rm(outputDirectory, { recursive: true, force: true })
  await mkdir(outputDirectory, { recursive: true })

  const images: { file: string, width: number, height: number, sha256: string }[] = []
  for (const screen of screens) {
    for (const appearance of appearances) {
      const context = await browser.newContext({
        viewport: screen.viewport,
        deviceScaleFactor: 1,
        locale: 'en-US',
        timezoneId: 'UTC',
        reducedMotion: 'reduce',
        colorScheme: appearance,
      })
      const page = await context.newPage()
      await page.goto(appURL)
      await expect(page.getByRole('heading', { name: 'Sample Marketplace', exact: true })).toBeVisible()
      await expect(page.locator('.marketplace-resource-row')).toHaveCount(5)
      await expect(page.locator('.resource-row__kind')).toHaveText(['Skill', 'Command', 'MCP server', 'Skill', 'Collection'])
      if (screen.name === 'desktop') {
        await expect(page.locator('.catalog-sidebar').getByText('Kind', { exact: true })).toBeVisible()
        await expect(page.locator('.catalog-sidebar').getByText('Registry', { exact: true })).toBeVisible()
      } else {
        await page.getByRole('button', { name: /^Filters/ }).click()
        const mobileFilters = page.locator('#mobile-resource-filters')
        await expect(mobileFilters.getByText('Kind', { exact: true })).toBeVisible()
        await expect(mobileFilters.getByText('Registry', { exact: true })).toBeVisible()
        await page.getByRole('button', { name: /^Filters/ }).click()
      }
      await expect(page.getByRole('button', { name: 'Copy install command for quality-check', exact: true })).toBeVisible()
      await page.addStyleTag({
        content: '*,:before,:after{animation:none!important;transition:none!important;caret-color:transparent!important}',
      })
      await page.evaluate(async () => { await document.fonts.ready })

      for (const view of ['catalog', 'detail'] as const) {
        if (view === 'detail') {
          const revision = `sha256:${'a'.repeat(64)}`
          await page.goto(`${appURL}/r/studio/skill/quality-check/revisions/${encodeURIComponent(revision)}`)
          await expect(page.getByTestId('revision-id')).toContainText('aaaaaaa')
          await page.getByRole('tab', { name: 'Security', exact: true }).click()
          await expect(page.getByRole('tabpanel', { name: 'Security' })).toContainText('Automated scan: no issues detected.')
          await expect(page.getByRole('tabpanel', { name: 'Security' })).toContainText('Automated analysis is limited and is not a safety endorsement.')
        }

        const file = `${view}-${screen.name}-${appearance}.webp`
        const temporaryPNG = join(tmpdir(), `landing-${process.pid}-${file}.png`)
        const temporaryPPM = join(tmpdir(), `landing-${process.pid}-${file}.ppm`)
        const output = join(outputDirectory, file)
        const bytes = await page.screenshot({
          path: temporaryPNG,
          type: 'png',
          scale: 'css',
          animations: 'disabled',
          caret: 'hide',
        })
        execFileSync(ffmpeg, ['-v', 'error', '-i', temporaryPNG, '-frames:v', '1', '-pix_fmt', 'rgb24', '-y', temporaryPPM])
        execFileSync(cwebp, ['-quiet', '-q', '82', '-m', '6', '-metadata', 'none', temporaryPPM, '-o', output])
        await rm(temporaryPNG, { force: true })
        await rm(temporaryPPM, { force: true })
        images.push({
          file,
          width: screen.viewport.width,
          height: screen.viewport.height,
          sha256: digest(await readFile(output)),
        })
        expect(bytes.length).toBeGreaterThan(0)
      }

      await context.close()
    }
  }

  await writeFile(
    join(outputDirectory, 'manifest.json'),
    `${JSON.stringify({
      fixture: {
        file: 'web/tests/e2e/fixtures/landing.json',
        sha256: digest(fixtureBytes),
      },
      images,
    }, null, 2)}\n`,
  )
})
