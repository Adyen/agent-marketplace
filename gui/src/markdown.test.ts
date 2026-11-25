// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'

function parse(source: string) {
  const container = document.createElement('div')
  container.innerHTML = renderMarkdown(source).html
  return container
}

describe('renderMarkdown', () => {
  it('renders headings, emphasis, inline code, fenced code, lists, and tables', () => {
    const source = [
      '# Title',
      '## Section',
      'Some *emphasis* and **strong** with `code`.',
      '',
      '```ts',
      'const a = 1',
      '```',
      '',
      '- one',
      '- two',
      '',
      '| a | b |',
      '| - | - |',
      '| 1 | 2 |',
    ].join('\n')
    const html = parse(source)
    expect(html.querySelector('h1')?.textContent).toBe('Title')
    expect(html.querySelector('h2')?.textContent).toBe('Section')
    expect(html.querySelector('em')?.textContent).toBe('emphasis')
    expect(html.querySelector('strong')?.textContent).toBe('strong')
    expect(html.querySelector('p code')?.textContent).toBe('code')
    expect(html.querySelector('pre code')?.textContent).toBe('const a = 1\n')
    expect(html.querySelectorAll('li')).toHaveLength(2)
    expect(html.querySelector('table td')?.textContent).toBe('1')
  })

  it('escapes raw HTML as text', () => {
    const html = parse('<script>alert(1)</script>\n\n<img src=x onerror="alert(2)">\n\n<iframe src="https://e"></iframe>')
    expect(html.querySelector('script')).toBeNull()
    expect(html.querySelector('img')).toBeNull()
    expect(html.querySelector('iframe')).toBeNull()
    expect(html.textContent).toContain('<script>alert(1)</script>')
    expect(html.textContent).toContain('<iframe src="https://e"></iframe>')
  })

  it('never emits an image element, only its escaped alt text', () => {
    const html = parse('![x](https://e/x.png)')
    expect(html.querySelector('img')).toBeNull()
    expect(html.querySelector('.resource-inspection__image-alt')?.textContent).toBe('x')
  })

  it.each([
    '[a](javascript:alert(1))',
    '[a](file:///etc/passwd)',
  ])('renders %s without an anchor', (source) => {
    const html = parse(source)
    expect(html.querySelector('a')).toBeNull()
    expect(html.textContent).toContain('a')
  })

  it('keeps an anchor for an https link', () => {
    const link = parse('[a](https://example.com)').querySelector('a')
    expect(link?.getAttribute('href')).toBe('https://example.com')
    expect(link?.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('splits leading frontmatter from the body', () => {
    const rendered = renderMarkdown('---\nname: solo\ndescription: Demo\n---\n# Body\n')
    expect(rendered.frontmatter).toBe('---\nname: solo\ndescription: Demo\n---')
    expect(rendered.html).toContain('<h1>Body</h1>')
    expect(rendered.html).not.toContain('name: solo')
  })

  it('reports no frontmatter when the file does not open with a closed block', () => {
    expect(renderMarkdown('# Body\n\n---\nnot: frontmatter\n').frontmatter).toBeNull()
    expect(renderMarkdown('---\nunclosed: true\n').frontmatter).toBeNull()
  })
})
