import { bench, describe, expect } from 'vitest'
import { highlightEscapedRawText } from '../../app/lib/markdown'
import type { RawTextFindingRange } from '../../app/lib/markdown'

describe('raw highlight CPU (no DOM or I/O)', () => {
  for (const [blocks, count, unicode] of [
    [16, 0, false],
    [16, 1, false],
    [4096, 0, false],
    [4096, 1, false],
    [4096, 256, false],
    [4096, 960, false],
    [4096, 512, true],
  ] as const) {
    const source = (unicode ? 'A\u00e9\u20ac\u{1f600}' : '0123456789abcdef').repeat(blocks)
    const ranges: RawTextFindingRange[] = Array.from({ length: count }, (_, index) => {
      const block = Math.floor(index * blocks / count)
      const startByte = unicode ? block * 10 + 6 : block * 16 + 15
      return Object.freeze({ index, startByte, endByte: startByte + (unicode ? 4 : 1), label: `finding ${index}` })
    }).reverse()
    Object.freeze(ranges)
    const actual = highlightEscapedRawText(source, ranges)
    expect(actual.match(/<mark /g)?.length ?? 0).toBe(count)
    if (!count) expect(actual).toBe(source)
    else {
      expect(actual).toContain(`id="finding-highlight-${count - 1}"`)
      expect(actual).toContain(unicode ? '>\u{1f600}</mark>' : '>f</mark>')
    }
    const bytes = new TextEncoder().encode(source).length
    bench(`${unicode ? 'unicode' : 'ascii'}_${bytes}bytes_${count}findings`, () => {
      highlightEscapedRawText(source, ranges)
    }, { time: 2000, iterations: 1, warmupTime: 200, warmupIterations: 1 })
  }
})
