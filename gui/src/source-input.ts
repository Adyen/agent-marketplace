import type { SourceInput } from './api/client'

export type SourceField = 'location' | 'ref' | 'path'
export type SourceFieldErrors = Partial<Record<SourceField, string>>

interface SourceDraft {
  kind: SourceInput['kind']
  location: string
  ref: string
  path: string
}

export function validateGitLocation(raw: string): boolean {
  if (/[\u0000-\u001f\u007f]/.test(raw) || raw.includes('\\')) return false

  const goWhitespace = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]/
  if (goWhitespace.test(raw[0] ?? '') || goWhitespace.test(raw.at(-1) ?? '')) return false

  const location = raw
  if (location === '' || location.startsWith('-') || location.includes('..')) return false

  const lower = location.toLowerCase()
  if (['ext::', 'remote::', 'helper::'].some((helper) => lower.includes(helper))) return false

  const schemeIndex = location.indexOf('://')
  if (schemeIndex >= 0) {
    const scheme = lower.slice(0, schemeIndex)
    const rest = location.slice(schemeIndex + 3)
    if (!['https', 'http', 'ssh', 'file'].includes(scheme)) return false
    if (scheme === 'file') return true

    let host = rest.split('/')[0]
    const at = host.lastIndexOf('@')
    if (at >= 0) host = host.slice(at + 1)
    const colon = host.lastIndexOf(':')
    if (colon > 0) host = host.slice(0, colon)
    return host !== '' && !host.startsWith('-')
  }

  if (lower.includes('::')) return false

  const at = location.indexOf('@')
  const scp = at >= 0 ? location.slice(at + 1) : location
  const colon = scp.indexOf(':')
  if (colon > 0) {
    const host = scp.slice(0, colon)
    const path = scp.slice(colon + 1)
    return !host.startsWith('-') && !path.startsWith('-')
  }

  return !scp.startsWith('~')
}

function isMarketplaceURL(location: string): boolean {
  const value = location.trim()
  if (!/^(https?):\/\/[^/?#\\]+/i.test(value)) return false

  try {
    const url = new URL(value)
    return (url.protocol === 'http:' || url.protocol === 'https:') && url.hostname !== ''
  } catch {
    return false
  }
}

export function validateSourceInput(source: SourceDraft): SourceFieldErrors {
  const errors: SourceFieldErrors = {}
  if (source.kind === 'directory') {
    if (source.location.trim() === '') errors.location = 'Enter a directory location.'
    return errors
  }
  if (source.kind === 'git') {
    if (!validateGitLocation(source.location)) errors.location = 'Enter a valid Git location.'
    if (source.ref.trim() === '') errors.ref = 'Enter a Git ref.'
    return errors
  }
  if (!isMarketplaceURL(source.location)) errors.location = 'Enter an absolute HTTP(S) URL.'
  return errors
}
