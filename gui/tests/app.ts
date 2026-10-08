import type { Page } from '@playwright/test'
import { existsSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { repoRoot } from './harness'

const dist = join(repoRoot, 'cmd', 'agent-marketplace-gui', 'frontend', 'dist')

const contentTypes: Record<string, string> = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
}

function ext(path: string): string {
  const index = path.lastIndexOf('.')
  return index === -1 ? '' : path.slice(index)
}

export function distIndexExists(): boolean {
  return existsSync(join(dist, 'index.html'))
}

export async function serveBuiltApp(page: Page, url: string) {
  await page.route(`${url}/**`, async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/')) {
      await route.continue()
      return
    }
    const relative = path === '/' ? 'index.html' : path.replace(/^\/+/, '')
    const file = join(dist, relative)
    if (!existsSync(file)) {
      await route.fulfill({ status: 404, body: 'not found' })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: contentTypes[ext(file)] ?? 'application/octet-stream',
      body: await readFile(file),
    })
  })
}
