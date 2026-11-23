import type { SourceDescription } from './api/client'

const SCP_LIKE = /^[^@/\s]+@[^:/\s]+:(.+)$/

function lastSegments(path: string, count = 2): string {
  return path.split('/').filter((segment) => segment !== '').slice(-count).join('/')
}

function marketplaceLabel(location: string): string {
  const url = new URL(location)
  if (url.hostname === '') {
    return location
  }
  const port = url.port === '' ? '' : `:${url.port}`
  const segment = lastSegments(url.pathname, 1)
  return `${url.hostname}${port}${segment === '' ? '' : `/${segment}`}`
}

export function hasNamedLocalLabel(source: SourceDescription): boolean {
  return source.kind !== 'marketplace' && source.name.trim() !== ''
}

export function sourceLabel(source: SourceDescription): string {
  const location = source.location
  if (hasNamedLocalLabel(source)) {
    return source.name.trim()
  }
  try {
    switch (source.kind) {
      case 'directory': {
        const path = location.startsWith('file://') ? decodeURIComponent(new URL(location).pathname) : location
        return lastSegments(path) || location
      }
      case 'marketplace':
        return marketplaceLabel(location)
      case 'git': {
        const scp = SCP_LIKE.exec(location)
        const path = scp ? scp[1] : new URL(location).pathname
        return lastSegments(path).replace(/\.git$/, '') || location
      }
    }
  } catch {
    return location
  }
  return location
}
