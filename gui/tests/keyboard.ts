import { expect, type Locator, type Page } from '@playwright/test'

export async function focusedDescription(page: Page): Promise<string> {
  const focused = page.locator(':focus')
  if ((await focused.count()) === 0) {
    return '<none>'
  }
  return focused.evaluate((element) => {
    const labels = element instanceof HTMLInputElement
      ? Array.from(element.labels ?? []).map((label) => label.textContent ?? '').join(' ')
      : ''
    const name = element.getAttribute('aria-label') ?? labels ?? element.textContent ?? ''
    const normalized = name.trim().replace(/\s+/g, ' ')
    const safeName = normalized.startsWith('Remove source ') ? 'Remove source <fixture>' : normalized
    const role = element.getAttribute('role') ?? element.tagName.toLowerCase()
    return `${role}${safeName === '' ? '' : ` "${safeName.slice(0, 80)}"`}`
  })
}

export async function tabTo(
  page: Page,
  target: Locator,
  targetName: string,
  actions: string[],
  scope?: Locator,
  key: 'Tab' | 'Shift+Tab' = 'Tab',
): Promise<void> {
  let lastFocus = '<none>'
  for (let step = 1; step <= 100; step++) {
    await page.keyboard.press(key)
    lastFocus = await focusedDescription(page)
    actions.push(`${key} ${step}: ${lastFocus}`)
    if (scope != null) {
      const remainsInScope = await scope.evaluate((element) => element.contains(document.activeElement))
      expect(remainsInScope, `Focus escaped ${targetName} dialog during ${key}`).toBe(true)
    }
    if (await target.evaluate((element) => element === document.activeElement)) {
      return
    }
  }
  throw new Error(`Could not reach "${targetName}" with ${key} within 100 Tab presses; last focus: ${lastFocus}`)
}
