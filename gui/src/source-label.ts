import type { SourceDescription } from './api/client'

const SCP_LIKE = /^[^@/\s]+@[^:/\s]+:(.+)$/
const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/

function lastSegments(path: string, count = 2): string {
  return path.split('/').filter((segment) => segment !== '').slice(-count).join('/')
}

function marketplaceLabel(location: string): string {
  const url = new URL(location)
  if (url.hostname === '') {
    return location
  }
  const host = IPV4.test(url.hostname) ? url.hostname : url.hostname.split('.')[0]
  const port = url.port === '' ? '' : `:${url.port}`
  const segment = lastSegments(url.pathname, 1)
  return `${host}${port}${segment === '' ? '' : `/${segment}`}`
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
