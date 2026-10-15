import type { SourceDescription } from './api/client'

const SCP_LIKE = /^[^@/\s]+@[^:/\s]+:(.+)$/

function lastSegments(path: string): string {
  return path.split('/').filter((segment) => segment !== '').slice(-2).join('/')
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
        return new URL(location).host || location
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
