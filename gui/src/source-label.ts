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

export function shortSourceLocation(location: string): string {
  try {
    const scp = SCP_LIKE.exec(location)
    let path = location
    if (scp) {
      path = scp[1]
    } else if (location.startsWith('file://')) {
      path = decodeURIComponent(new URL(location).pathname)
    } else if (location.includes('://')) {
      path = new URL(location).pathname
    }
    return lastSegments(path).replace(/\.git$/, '') || location
  } catch {
    return location
  }
}

export function sourceDisplayLabel(source: SourceDescription, rowLabel = ''): string {
  if (hasNamedLocalLabel(source)) {
    return source.name.trim()
  }
  return rowLabel || sourceLabel(source)
}

export function sourceLabel(source: SourceDescription): string {
  const location = source.location
  if (hasNamedLocalLabel(source)) {
    return source.name.trim()
  }
  if (source.kind === 'marketplace') {
    try {
      return marketplaceLabel(location)
    } catch {
      return location
    }
  }
  return shortSourceLocation(location)
}
