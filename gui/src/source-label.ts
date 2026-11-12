import type { SourceDescription } from './api/client'

const SCP_LIKE = /^[^@/\s]+@[^:/\s]+:(.+)$/
const GENERIC_MARKETPLACE_NAME = /^marketplace(-\d+)?$/

function lastSegments(path: string, count = 2): string {
  return path.split('/').filter((segment) => segment !== '').slice(-count).join('/')
}

function marketplaceLabel(source: SourceDescription): string {
  if (source.name !== '' && !GENERIC_MARKETPLACE_NAME.test(source.name)) {
    return source.name
  }
  const url = new URL(source.location)
  if (url.hostname === '') {
    return source.location
  }
  const port = url.port === '' ? '' : `:${url.port}`
  const segment = lastSegments(url.pathname, 1)
  return `${url.hostname}${port}${segment === '' ? '' : `/${segment}`}`
}

export function sourceLabel(source: SourceDescription): string {
  const location = source.location
  try {
    switch (source.kind) {
      case 'directory': {
        const path = location.startsWith('file://') ? decodeURIComponent(new URL(location).pathname) : location
        return lastSegments(path) || location
      }
      case 'marketplace':
        return marketplaceLabel(source)
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
