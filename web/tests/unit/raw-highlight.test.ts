import { describe, expect, it } from 'vitest'
import { highlightEscapedRawText } from '../../app/lib/markdown'

describe('raw finding byte-boundary preservation', () => {
  it.each([
    [0, 1, '', 'A', '\u00e9\u20ac\u{1f600}Z'],
    [1, 3, 'A', '\u00e9', '\u20ac\u{1f600}Z'],
    [3, 6, 'A\u00e9', '\u20ac', '\u{1f600}Z'],
    [6, 10, 'A\u00e9\u20ac', '\u{1f600}', 'Z'],
    [10, 11, 'A\u00e9\u20ac\u{1f600}', 'Z', ''],
    [0, 11, '', 'A\u00e9\u20ac\u{1f600}Z', ''],
    [1, 10, 'A', '\u00e9\u20ac\u{1f600}', 'Z'],
  ] as const)('preserves exact UTF-8 endpoints %s..%s', (startByte, endByte, prefix, text, suffix) => {
    expect(highlightEscapedRawText('A\u00e9\u20ac\u{1f600}Z', [{ index: 7, startByte, endByte }]))
      .toBe(`${prefix}<mark id="finding-highlight-7" tabindex="-1" aria-label="Finding highlight">${text}</mark>${suffix}`)
  })

  it.each([
    [undefined, 3], [1, undefined], [NaN, 3], [1, NaN],
    [Infinity, 3], [1, Infinity], [-1, 3], [0, -1],
    [1.5, 3], [1, 3.5], [2, 3], [1, 2], [4, 6],
    [3, 5], [7, 10], [6, 9], [0, 12], [12, 13],
    [Number.MAX_SAFE_INTEGER + 1, Number.MAX_VALUE], [3, 3], [3, 1],
  ])('rejects invalid endpoint pair %s..%s without losing a valid finding', (startByte, endByte) => {
    const actual = highlightEscapedRawText('A\u00e9\u20ac\u{1f600}Z', [
      { index: 8, startByte, endByte },
      { index: 4, startByte: 10, endByte: 11 },
    ])
    expect(actual).toBe('A\u00e9\u20ac\u{1f600}<mark id="finding-highlight-4" tabindex="-1" aria-label="Finding highlight">Z</mark>')
  })

  it('retains stable ties, overlap label order, adjacent marks and escaped labels', () => {
    const ranges = Object.freeze([
      Object.freeze({ index: 9, startByte: 1, endByte: 4, label: 'A' }),
      Object.freeze({ index: 3, startByte: 1, endByte: 3, label: 'B' }),
      Object.freeze({ index: 2, startByte: 1, endByte: 3, label: 'B' }),
      Object.freeze({ index: 7, startByte: 2, endByte: 5, label: 'A' }),
      Object.freeze({ index: 8, startByte: 5, endByte: 6, label: '" onmouseover="alert(1) <img>' }),
    ])
    expect(highlightEscapedRawText('abcdef', ranges)).toBe('a<mark id="finding-highlight-3" tabindex="-1" aria-label="Finding highlight: B; A">bcde</mark><mark id="finding-highlight-8" tabindex="-1" aria-label="Finding highlight: &quot; onmouseover=&quot;alert(1) &lt;img&gt;">f</mark>')
    expect(ranges.map(range => range.index)).toEqual([9, 3, 2, 7, 8])
  })

  it.each([-1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])('keeps the safe ID fallback for %s', index => {
    expect(highlightEscapedRawText('<&>', [{ index, startByte: 0, endByte: 3, label: '' }]))
      .toBe('<mark id="finding-highlight-0" tabindex="-1" aria-label="Finding highlight">&lt;&amp;&gt;</mark>')
  })

  it('preserves sanitized-source byte offsets rather than remapping them', () => {
    expect(highlightEscapedRawText('\u202eA\u0000\u00e9', [
      { index: 1, startByte: 1, endByte: 3 },
      { index: 2, startByte: 4, endByte: 6 },
    ])).toBe('A<mark id="finding-highlight-1" tabindex="-1" aria-label="Finding highlight">\u00e9</mark>')
  })

  it('retains TextEncoder replacement-byte boundaries for lone surrogates', () => {
    expect(highlightEscapedRawText('\ud800x\udc00', [
      { index: 1, startByte: 0, endByte: 3 },
      { index: 2, startByte: 4, endByte: 7 },
      { index: 3, startByte: 5, endByte: 7 },
    ])).toBe('<mark id="finding-highlight-1" tabindex="-1" aria-label="Finding highlight">\ud800</mark>x<mark id="finding-highlight-2" tabindex="-1" aria-label="Finding highlight">\udc00</mark>')
  })

  it('preserves empty/escaped source when there is nothing valid to mark', () => {
    expect(highlightEscapedRawText('', [{ index: 0, startByte: 0, endByte: 0 }])).toBe('')
    expect(highlightEscapedRawText('<script>&"\'', [])).toBe('&lt;script&gt;&amp;&quot;\'')
  })
})
