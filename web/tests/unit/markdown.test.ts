import { describe, expect, it } from 'vitest'
import { escapeRawText, renderSafeMarkdown } from '../../app/lib/markdown'

describe('safe Markdown rendering', () => {
  it('never activates raw script tags or event handlers', () => {
    const html = renderSafeMarkdown('<script>alert(1)</script>\n<img src=x onerror="alert(2)">\n<div onclick="alert(3)">click</div>')

    expect(html).not.toMatch(/<(?:script|img|div)\b/i)
    expect(html).not.toMatch(/<[^>]+\son\w+=/i)
    expect(html).toContain('&lt;script&gt;')
  })

  it.each([
    'javascript:alert(1)',
    'JaVaScRiPt:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox(1)',
    '//evil.example/path',
  ])('does not create a link for %s', destination => {
    const html = renderSafeMarkdown(`[unsafe](${destination})`)
    expect(html).not.toContain('<a ')
    expect(html).not.toContain('href=')
  })

  it('does not emit image elements or remote image URLs', () => {
    const html = renderSafeMarkdown('before ![tracking pixel](https://evil.example/pixel.png) after')
    expect(html).toContain('tracking pixel')
    expect(html).not.toContain('<img')
    expect(html).not.toContain('evil.example')
  })

  it('removes bidi overrides and unsafe control characters', () => {
    const hostile = 'safe\u202Etxt.exe\u2066\u0000\u0007end'
    expect(renderSafeMarkdown(hostile)).toContain('safetxt.exeend')
    for (const unsafeCharacter of ['\u0000', '\u0007', '\u202E', '\u2066']) {
      expect(renderSafeMarkdown(hostile)).not.toContain(unsafeCharacter)
    }
    expect(escapeRawText(hostile)).toBe('safetxt.exeend')
  })

  it('keeps safe headings, lists, code, emphasis, and HTTP links', () => {
    const html = renderSafeMarkdown('# Heading\n\n- one\n- **two**\n\n`inline`\n\n```ts\nconst value = "<safe>"\n```\n\n[docs](https://example.com/docs "Docs")')

    expect(html).toContain('<h1>Heading</h1>')
    expect(html).toContain('<ul>')
    expect(html).toContain('<strong>two</strong>')
    expect(html).toContain('<code>inline</code>')
    expect(html).toContain('<pre><code>const value = "&lt;safe&gt;"')
    expect(html).toContain('<a href="https://example.com/docs" title="Docs">docs</a>')
  })

  it('escapes raw text without interpreting it as Markdown or HTML', () => {
    expect(escapeRawText('<b>**not bold** & "quoted"</b>')).toBe('&lt;b&gt;**not bold** &amp; &quot;quoted&quot;&lt;/b&gt;')
  })
})
