import { expect, test } from '@playwright/test'

const digest = `sha256:${'a'.repeat(64)}`
const heading = '# Wide raw preview\n\n'
const prefix = `${heading}\u{1f600}\u00e9\n`
const source = `${prefix}${'0123456789abcdef'.repeat(4096)}\n<script>alert(1)</script><img src="https://evil.invalid/pixel">`
const count = 960
const findings = Array.from({ length: count }, (_, index) => ({
  code: 'remote-reference',
  severity: 'warn',
  location: { path: 'SKILL.md', start_byte: Buffer.byteLength(prefix) + index * 64 + 15, end_byte: Buffer.byteLength(prefix) + index * 64 + 16 },
})).concat([
  { code: 'unicode-reference', severity: 'warn', location: { path: 'SKILL.md', start_byte: heading.length, end_byte: heading.length + 4 } },
  { code: 'split-utf8', severity: 'warn', location: { path: 'SKILL.md', start_byte: heading.length + 1, end_byte: heading.length + 4 } },
])

test('renders every valid raw finding through the digest-pinned browser consumer', async ({ page, browser }, testInfo) => {
  const requests: string[] = []
  const dialogs: string[] = []
  page.on('request', request => requests.push(request.url()))
  page.on('dialog', async (dialog) => {
    dialogs.push(dialog.message())
    await dialog.dismiss()
  })
  await page.route('**/api/v1/resources/team-tools/skill/secure-review/revisions/**', async (route) => {
    const path = decodeURIComponent(new URL(route.request().url()).pathname)
    const response = await route.fetch()
    if (path.endsWith('/inspection/content') || path.endsWith(`/inspection/content/sha256:${'e'.repeat(64)}`)) {
      return route.fulfill({ response, contentType: 'text/markdown', body: source })
    }
    const body = await response.json()
    if (path.endsWith('/inspection')) body.files[0].size = Buffer.byteLength(source)
    if (path.endsWith(`/revisions/${digest}`)) body.assessment.findings = findings
    return route.fulfill({ response, json: body })
  })

  const milliseconds: number[] = []
  for (let sample = 0; sample < 5; sample++) {
    // Fresh navigation creates a new composable; Raw must not reuse a warm
    // highlightedContent computed value from the preceding sample.
    await page.goto('/')
    await page.waitForFunction(() => {
      const app = document.querySelector('#__nuxt')
      return app && '__vue_app__' in app
    })
    await page.getByRole('link', { name: 'team-tools/secure-review', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/revisions/${encodeURIComponent(digest)}$`))
    await expect(page.getByRole('heading', { name: 'Wide raw preview', exact: true })).toBeVisible()
    const rawButton = page.getByRole('button', { name: 'Raw', exact: true })
    await expect(rawButton).toBeVisible()
    const before = requests.length
    milliseconds.push(await rawButton.evaluate(button => new Promise<number>((resolve) => {
      const start = performance.now()
      button.click()
      requestAnimationFrame(() => requestAnimationFrame(() => resolve(performance.now() - start)))
    })))
    expect(requests.slice(before)).toEqual([])
    const raw = page.getByTestId('escaped-raw')
    const marks = raw.locator('mark')
    await expect(marks).toHaveCount(count + 1)
    expect(await raw.textContent()).toBe(source)
    expect(await marks.allTextContents()).toEqual(['\u{1f600}', ...Array<string>(count).fill('f')])
    await expect(marks.first()).toHaveAttribute('id', `finding-highlight-${count}`)
    await expect(marks.last()).toHaveAttribute('id', `finding-highlight-${count - 1}`)
    await expect(marks.last()).toHaveAttribute('aria-label', 'Finding highlight: remote-reference (warn)')
    await expect(marks.last()).toHaveAttribute('tabindex', '-1')
    await expect(raw.locator(`#finding-highlight-${count + 1}`)).toHaveCount(0)
    await expect(raw.locator('script, img, [onerror], [onmouseover]')).toHaveCount(0)
  }
  expect(dialogs).toEqual([])
  expect(requests.filter(url => url.includes('evil.invalid'))).toEqual([])
  await testInfo.attach('raw-highlight-browser-timings', {
    body: JSON.stringify({
      browser: browser.version(),
      sourceBytes: Buffer.byteLength(source),
      findings: findings.length,
      validMarks: count + 1,
      milliseconds,
      scope: 'Cold Raw click through Vue computed, DOM update and two animation frames; navigation/API setup excluded, no requests during timing. Not an isolated helper or full-page-load benchmark.',
    }),
    contentType: 'application/json',
  })
})
