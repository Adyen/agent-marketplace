import { existsSync } from 'node:fs'
import { cp, mkdir, readdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join, relative, sep } from 'node:path'
import type { DefaultTheme } from 'vitepress'

interface Page {
  label: string
  rel: string
}

interface Section {
  heading: string
  pages: Page[]
}

interface LlmsOptions {
  base: string
  description: string
  docsPath: string
  outDir: string
  sidebar: DefaultTheme.SidebarItem[]
  srcDir: string
  title: string
}

function collectSections(items: DefaultTheme.SidebarItem[], docsPath: string, trail: string[] = [], sections: Section[] = []): Section[] {
  for (const item of items) {
    if (item.link) {
      if (!item.link.startsWith(docsPath)) throw new Error(`sidebar link is outside ${docsPath}: ${item.link}`)
      const rest = item.link.slice(docsPath.length)
      const rel = rest === '' || rest.endsWith('/') ? `${rest}index` : rest
      const heading = trail.join(' / ') || 'Docs'
      const page = { label: item.text ?? rel, rel }
      const last = sections.at(-1)
      if (last?.heading === heading) last.pages.push(page)
      else sections.push({ heading, pages: [page] })
    }
    if (item.items) collectSections(item.items, docsPath, [...trail, item.text ?? ''], sections)
  }
  return sections
}

async function markdownFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true })
  const files = await Promise.all(entries.map(entry => {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) return entry.name === 'images' ? [] : markdownFiles(path)
    return entry.name.endsWith('.md') ? [path] : []
  }))
  return files.flat()
}

export function stripFrontMatter(markdown: string): string {
  return markdown.replace(/^---\r?\n(?:[\s\S]*?\r?\n)?---\r?\n/, '')
}

// Writes llms.txt and a Markdown copy of each sidebar page beside its HTML route.
export async function writeLlmsFiles({ base, description, docsPath, outDir, sidebar, srcDir, title }: LlmsOptions): Promise<void> {
  const sections = collectSections(sidebar, docsPath)
  const sourceRoot = join(srcDir, 'docs')
  const docsOutput = docsPath.slice(1)
  const outputRoot = join(outDir, docsOutput)

  const listed = new Set<string>()
  for (const { rel } of sections.flatMap(section => section.pages)) {
    const source = join(sourceRoot, `${rel}.md`)
    if (!existsSync(source)) throw new Error(`sidebar link has no source page: ${source}`)
    listed.add(source)
  }
  const unlisted = (await markdownFiles(sourceRoot)).filter(file => !listed.has(file))
  if (unlisted.length) {
    const names = unlisted.map(file => relative(sourceRoot, file).split(sep).join('/'))
    throw new Error(`documentation pages missing from the sidebar (add them to docsSidebar): ${names.join(', ')}`)
  }

  for (const { rel } of sections.flatMap(section => section.pages)) {
    const markdown = await readFile(join(sourceRoot, `${rel}.md`), 'utf8')
    const target = join(outputRoot, `${rel}.md`)
    await mkdir(dirname(target), { recursive: true })
    await writeFile(target, stripFrontMatter(markdown))
  }
  await cp(join(sourceRoot, 'images'), join(outputRoot, 'images'), { recursive: true })

  const lines = [`# ${title}`, '', `> ${description}`, '', 'Each link points to the Markdown source of a documentation page.', '']
  for (const section of sections) {
    lines.push(`## ${section.heading}`)
    for (const page of section.pages) lines.push(`- [${page.label}](${base}${docsOutput}${page.rel}.md)`)
    lines.push('')
  }
  await writeFile(join(outDir, 'llms.txt'), lines.join('\n'))
}
