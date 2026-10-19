import MarkdownIt from 'markdown-it'
import sanitizeHtml from 'sanitize-html'

const URI_SCHEME = /^([a-z][a-z\d+.-]*):/i

const markdown = new MarkdownIt({
  html: false,
  linkify: false,
  typographer: false,
})

markdown.validateLink = (url: string): boolean => {
  const value = url.trim()
  if (!value || hasUnsafeFormattingCharacter(value) || value.includes('\\') || value.startsWith('//')) return false
  const scheme = URI_SCHEME.exec(value)?.[1]?.toLowerCase()
  return scheme === undefined || scheme === 'http' || scheme === 'https'
}

// Links are the only active elements emitted by the renderer. Keep them
// isolated from the Marketplace window and make that behavior explicit to
// assistive technology without weakening the HTTP(S)-only URL policy above.
markdown.renderer.rules.link_open = (tokens, index, options, env, self) => {
  const token = tokens[index]
  if (!token) return ''
  token.attrSet('target', '_blank')
  token.attrSet('rel', 'noopener noreferrer')
  return self.renderToken(tokens, index, options)
}

// Content API Markdown must not trigger network requests. Preserve image alt
// text as escaped text while discarding both local and remote image sources.
markdown.renderer.rules.image = (tokens, index) => markdown.utils.escapeHtml(tokens[index]?.content ?? '')

const SANITIZE_OPTIONS: sanitizeHtml.IOptions = {
  allowedTags: [
    'a', 'blockquote', 'br', 'code', 'em', 'h1', 'h2', 'h3', 'h4',
    'h5', 'h6', 'hr', 'li', 'ol', 'p', 'pre', 'strong', 'ul',
  ],
  allowedAttributes: {
    a: ['href', 'title', 'target', 'rel'],
  },
  allowedSchemes: ['http', 'https'],
  allowProtocolRelative: false,
  enforceHtmlBoundary: true,
}

/** Render untrusted Markdown into a small, sanitized HTML subset. */
export function renderSafeMarkdown(source: string): string {
  const boundedSource = stripUnsafeFormattingCharacters(source)
  return sanitizeHtml(markdown.render(boundedSource), SANITIZE_OPTIONS)
}

/** Escape untrusted plain text for insertion into an HTML string. */
export function escapeRawText(source: string): string {
  return markdown.utils.escapeHtml(stripUnsafeFormattingCharacters(source))
}

export interface RawTextFindingRange {
  index: number
  startByte?: number
  endByte?: number
  label?: string
}

/**
 * Escape raw content and add inert, accessible marks for valid UTF-8 byte
 * ranges. The source is always escaped before the static mark element is
 * added, so findings cannot turn this preview into an HTML sink.
 */
export function highlightEscapedRawText(source: string, ranges: readonly RawTextFindingRange[]): string {
  const cleanSource = stripUnsafeFormattingCharacters(source)
  const boundaries = utf8Boundaries(cleanSource)
  const validRanges = ranges
    .map(range => ({
      ...range,
      start: byteBoundary(boundaries, range.startByte),
      end: byteBoundary(boundaries, range.endByte),
    }))
    .filter((range): range is typeof range & { start: number; end: number } => range.start !== undefined && range.end !== undefined && range.end > range.start)
    .sort((left, right) => left.start - right.start || left.end - right.end)

  if (!validRanges.length) return markdown.utils.escapeHtml(cleanSource)

  const output: string[] = []
  let cursor = 0
  let rangeIndex = 0
  while (rangeIndex < validRanges.length) {
    const first = validRanges[rangeIndex]
    if (!first) break
    if (first.start > cursor) output.push(markdown.utils.escapeHtml(cleanSource.slice(cursor, first.start)))
    const overlapping = [first]
    let end = first.end
    rangeIndex += 1
    while (rangeIndex < validRanges.length) {
      const next = validRanges[rangeIndex]
      if (!next || next.start >= end) break
      overlapping.push(next)
      end = Math.max(end, next.end)
      rangeIndex += 1
    }
    const labels = [...new Set(overlapping.map(range => range.label).filter((label): label is string => Boolean(label)))]
    const label = labels.length ? `Finding highlight: ${labels.join('; ')}` : 'Finding highlight'
    const highlightIndex = Number.isSafeInteger(first.index) && first.index >= 0 ? first.index : 0
    output.push(`<mark id="finding-highlight-${highlightIndex}" tabindex="-1" aria-label="${markdown.utils.escapeHtml(label)}">${markdown.utils.escapeHtml(cleanSource.slice(first.start, end))}</mark>`)
    cursor = end
  }
  if (cursor < cleanSource.length) output.push(markdown.utils.escapeHtml(cleanSource.slice(cursor)))
  return output.join('')
}

function utf8Boundaries(source: string): Array<{ byte: number; index: number }> {
  const encoder = new TextEncoder()
  const boundaries = [{ byte: 0, index: 0 }]
  let byte = 0
  let index = 0
  for (const character of source) {
    byte += encoder.encode(character).length
    index += character.length
    boundaries.push({ byte, index })
  }
  return boundaries
}

function byteBoundary(boundaries: Array<{ byte: number; index: number }>, byte: number | undefined): number | undefined {
  if (byte === undefined || !Number.isInteger(byte) || byte < 0) return undefined
  let lower = 0
  let upper = boundaries.length - 1
  while (lower <= upper) {
    const middle = lower + Math.floor((upper - lower) / 2)
    const boundary = boundaries[middle]
    if (!boundary) return undefined
    if (boundary.byte === byte) return boundary.index
    if (boundary.byte < byte) lower = middle + 1
    else upper = middle - 1
  }
  return undefined
}

function stripUnsafeFormattingCharacters(source: string): string {
  return [...source].filter(character => !isUnsafeFormattingCharacter(character.codePointAt(0) ?? 0)).join('')
}

function hasUnsafeFormattingCharacter(source: string): boolean {
  return [...source].some(character => isUnsafeFormattingCharacter(character.codePointAt(0) ?? 0))
}

function isUnsafeFormattingCharacter(codePoint: number): boolean {
  return codePoint <= 0x08
    || (codePoint >= 0x0b && codePoint <= 0x0c)
    || (codePoint >= 0x0e && codePoint <= 0x1f)
    || (codePoint >= 0x7f && codePoint <= 0x9f)
    || codePoint === 0x061c
    || codePoint === 0x200e
    || codePoint === 0x200f
    || (codePoint >= 0x202a && codePoint <= 0x202e)
    || (codePoint >= 0x2066 && codePoint <= 0x2069)
}
