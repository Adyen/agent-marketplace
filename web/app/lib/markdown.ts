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

// Content API Markdown must not trigger network requests. Preserve image alt
// text as escaped text while discarding both local and remote image sources.
markdown.renderer.rules.image = (tokens, index) => markdown.utils.escapeHtml(tokens[index]?.content ?? '')

const SANITIZE_OPTIONS: sanitizeHtml.IOptions = {
  allowedTags: [
    'a', 'blockquote', 'br', 'code', 'em', 'h1', 'h2', 'h3', 'h4',
    'h5', 'h6', 'hr', 'li', 'ol', 'p', 'pre', 'strong', 'ul',
  ],
  allowedAttributes: {
    a: ['href', 'title'],
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
