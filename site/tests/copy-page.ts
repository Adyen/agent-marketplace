import assert from 'node:assert/strict'
import { access } from 'node:fs/promises'

function elementsWithAttribute(html: string, tag: string, attribute: string): string[] {
  return [...html.matchAll(new RegExp(
    `<${tag}\\b(?=[^>]*\\b${attribute}(?:[=\\s>]))[^>]*>[\\s\\S]*?<\\/${tag}>`,
    'g',
  ))].map(([element]) => element)
}

export function assertCopyPageTrigger(html: string, page: string): void {
  const triggers = elementsWithAttribute(html, 'button', 'data-copy-page-trigger')
  assert.equal(triggers.length, 1, `${page} should contain exactly one Copy page trigger`)
  assert.match(triggers[0], /\baria-haspopup="menu"/)
  assert.match(triggers[0], /\baria-expanded="false"/)
  assert.match(triggers[0], /Copy page/)
}

export function assertNoCopyPageTrigger(html: string, page: string): void {
  assert.equal(elementsWithAttribute(html, 'button', 'data-copy-page-trigger').length, 0, `${page} should not contain a Copy page trigger`)
}

export async function assertCopyPageView(
  html: string,
  route: string,
  base: string,
  outputRoot: URL,
  page: string,
): Promise<void> {
  const links = elementsWithAttribute(html, 'a', 'data-copy-page-view')
  assert.equal(links.length, 1, `${page} should contain exactly one View as Markdown link`)

  const link = links[0]
  assert.match(link, /\btarget="_blank"/)
  assert.match(link, /View as Markdown/)
  const rel = link.match(/\brel="([^"]+)"/)?.[1].split(/\s+/) ?? []
  assert.ok(rel.includes('noopener'), `${page} View as Markdown link should include rel="noopener"`)

  const href = link.match(/\bhref="([^"]+)"/)?.[1]
  assert.ok(href, `${page} View as Markdown link should have an href`)
  const url = new URL(href, 'https://copy-page.invalid')
  const normalizedBase = base.endsWith('/') ? base : `${base}/`
  const markdownRoute = route.replace(/\.html$/, '.md')
  assert.equal(url.pathname, `${normalizedBase}${markdownRoute}`, `${page} View as Markdown href`)
  await access(new URL(decodeURIComponent(url.pathname.slice(normalizedBase.length)), outputRoot))
}
