import MarkdownIt from 'markdown-it'
import type Token from 'markdown-it/lib/token.mjs'

export interface RenderedMarkdown {
  frontmatter: string | null
  html: string
}

interface RenderEnv {
  links: boolean[]
}

const frontmatterPattern = /^---\r?\n(?:[\s\S]*?\r?\n)?---[ \t]*(?:\r?\n|$)/

const markdown = new MarkdownIt({ html: false, linkify: false, typographer: false })

function isExternalHref(href: string | null): boolean {
  if (href == null) {
    return false
  }
  try {
    const protocol = new URL(href).protocol
    return protocol === 'http:' || protocol === 'https:'
  } catch {
    return false
  }
}

markdown.renderer.rules.image = (tokens: Token[], index: number, _options, _env, self) => {
  const alt = self.renderInlineAsText(tokens[index]?.children ?? [], _options, _env)
  return `<span class="resource-inspection__image-alt">${markdown.utils.escapeHtml(alt)}</span>`
}

markdown.renderer.rules.link_open = (tokens: Token[], index: number, _options, env: RenderEnv) => {
  const href = tokens[index]?.attrGet('href') ?? null
  const external = isExternalHref(href)
  env.links.push(external)
  if (!external || href == null) {
    return '<span class="resource-inspection__plain-link">'
  }
  return `<a href="${markdown.utils.escapeHtml(href)}" rel="noopener noreferrer">`
}

markdown.renderer.rules.link_close = (_tokens: Token[], _index: number, _options, env: RenderEnv) =>
  env.links.pop() ? '</a>' : '</span>'

export function renderMarkdown(source: string): RenderedMarkdown {
  const match = frontmatterPattern.exec(source)
  if (match == null) {
    return { frontmatter: null, html: markdown.render(source, { links: [] } satisfies RenderEnv) }
  }
  return {
    frontmatter: match[0].replace(/\r?\n$/, ''),
    html: markdown.render(source.slice(match[0].length), { links: [] } satisfies RenderEnv),
  }
}
