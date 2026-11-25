import type { SourceInput } from './api/client'

const v4GitScpRE = /^(?:[^@\t\n\f\r \/:]+@)?[^/\t\n\f\r :]+:[^\t\n\f\r ]+$/

export type SourceField = 'location' | 'ref'
export type SourceFieldErrors = Partial<Record<SourceField, string>>

interface SourceDraft {
  kind: SourceInput['kind']
  location: string
  ref: string
  path: string
}

function isGoWhitespace(code: number): boolean {
  return code === 0x09 || code === 0x0a || code === 0x0b || code === 0x0c || code === 0x0d ||
    code === 0x20 || code === 0x85 || code === 0xa0 || code === 0x1680 ||
    (code >= 0x2000 && code <= 0x200a) || code === 0x2028 || code === 0x2029 ||
    code === 0x202f || code === 0x205f || code === 0x3000
}

function trimGoWhitespace(value: string): string {
  let start = 0
  let end = value.length
  while (start < end && isGoWhitespace(value.charCodeAt(start))) start++
  while (end > start && isGoWhitespace(value.charCodeAt(end - 1))) end--
  return value.slice(start, end)
}

function hasInvalidEscape(value: string): boolean {
  return /%(?![0-9a-f]{2})/i.test(value)
}

function isGoHostCharacter(character: string): boolean {
  return /^[A-Za-z0-9._~!$&'()*+,;=:\[\]<>"-]$/.test(character)
}

function validGoHostCharacters(value: string): boolean {
  for (let index = 0; index < value.length; index++) {
    const character = value[index]
    if (character === '%') {
      const escape = value.slice(index, index + 3)
      if (hasInvalidEscape(escape) || (Number.parseInt(value[index + 1], 16) < 8 && escape.toUpperCase() !== '%25')) {
        return false
      }
      index += 2
    } else if (character.charCodeAt(0) < 0x80 && !isGoHostCharacter(character)) {
      return false
    }
  }
  return true
}

function validGoUserInfo(value: string): boolean {
  return !hasInvalidEscape(value) && /^[A-Za-z0-9._~!$&'()*+,;=%@-]*$/.test(value)
}

function validGoZone(value: string): boolean {
  if (value === '') return false
  for (let index = 0; index < value.length; index++) {
    const character = value[index]
    if (character === '%') {
      const escape = value.slice(index, index + 3)
      if (hasInvalidEscape(escape)) return false
      const escaped = Number.parseInt(value.slice(index + 1, index + 3), 16)
      if (escape.toUpperCase() !== '%25' && escaped !== 0x20 && escaped < 0x80 && !isGoHostCharacter(String.fromCharCode(escaped))) {
        return false
      }
      index += 2
    } else if (
      character.charCodeAt(0) < 0x80 &&
      !isGoHostCharacter(character)
    ) {
      return false
    }
  }
  return true
}

function validGoURLHostPort(hostPort: string, scheme: string): boolean {
  if (hostPort.startsWith('[')) {
    const closingBracket = hostPort.lastIndexOf(']')
    if (closingBracket < 0 || hostPort.indexOf('[', 1) >= 0 || !/^(:\d*)?$/.test(hostPort.slice(closingBracket + 1))) {
      return false
    }
    let address = hostPort.slice(1, closingBracket)
    const zoneIndex = address.indexOf('%25')
    if (zoneIndex >= 0) {
      if (!validGoZone(address.slice(zoneIndex + 3))) return false
      address = address.slice(0, zoneIndex)
    } else if (address.includes('%')) {
      return false
    }
    try {
      new URL(`http://[${address}]/`)
      return true
    } catch {
      return false
    }
  }
  if (hostPort.includes('[')) return false

  const firstColon = hostPort.indexOf(':')
  if (firstColon >= 0) {
    const portIndex = hostPort.lastIndexOf(':')
    const port = hostPort.slice(portIndex)
    if (!/^:\d*$/.test(port)) return false
    if (firstColon !== portIndex && (scheme === 'http' || scheme === 'https')) return false
  }
  return validGoHostCharacters(hostPort)
}

function validGoGitURL(location: string, scheme: string): boolean {
  let urlPart = location.slice(scheme.length + 1)
  const fragmentIndex = urlPart.indexOf('#')
  if (fragmentIndex >= 0) {
    if (urlPart.slice(fragmentIndex + 1) !== '') return false
    urlPart = urlPart.slice(0, fragmentIndex)
  }
  const queryIndex = urlPart.indexOf('?')
  if (queryIndex >= 0) {
    if (urlPart.slice(queryIndex + 1) !== '') return false
    urlPart = urlPart.slice(0, queryIndex)
  }

  if (scheme === 'file') {
    if (!urlPart.startsWith('///')) return false
    const path = urlPart.slice(2)
    return path.startsWith('/') && !hasInvalidEscape(path)
  }
  if (!urlPart.startsWith('//') || urlPart.startsWith('///')) return false

  const remainder = urlPart.slice(2)
  const pathIndex = remainder.indexOf('/')
  const authority = pathIndex < 0 ? remainder : remainder.slice(0, pathIndex)
  const path = pathIndex < 0 ? '' : remainder.slice(pathIndex)
  if (authority === '' || hasInvalidEscape(path)) return false

  const userIndex = authority.lastIndexOf('@')
  let hostPort = authority
  if (userIndex >= 0) {
    const userInfo = authority.slice(0, userIndex)
    if (scheme !== 'ssh' || userInfo.includes(':') || !validGoUserInfo(userInfo)) return false
    hostPort = authority.slice(userIndex + 1)
  }
  let safetyHost = hostPort
  const lastColon = safetyHost.lastIndexOf(':')
  if (lastColon > 0) safetyHost = safetyHost.slice(0, lastColon)
  if (safetyHost === '' || safetyHost.startsWith('-')) return false
  return validGoURLHostPort(hostPort, scheme)
}

export function validateGitLocation(raw: string): boolean {
  const location = trimGoWhitespace(raw)
  if (/[\u0000-\u001f\u007f]/.test(location) || location.includes('\\')) return false
  if (location === '' || location.startsWith('-') || location.includes('..')) return false

  const lower = location.toLowerCase()
  if (['ext::', 'remote::', 'helper::'].some((helper) => lower.includes(helper))) return false

  const schemeIndex = location.indexOf('://')
  if (schemeIndex >= 0) {
    const scheme = lower.slice(0, schemeIndex)
    if (!['https', 'http', 'ssh', 'file'].includes(scheme)) return false
    return validGoGitURL(location, scheme)
  }

  if (lower.includes('::')) return false

  const firstAt = location.indexOf('@')
  if (firstAt >= 0 && location.slice(0, firstAt).includes(':')) return false
  const scp = firstAt >= 0 ? location.slice(firstAt + 1) : location
  const colon = scp.indexOf(':')
  if (colon > 0) {
    const host = scp.slice(0, colon)
    const path = scp.slice(colon + 1)
    if (host.startsWith('-') || path.startsWith('-')) return false
  } else if (scp.startsWith('~')) {
    return false
  }

  if (location.startsWith('/')) {
    const fragmentIndex = location.indexOf('#')
    const beforeFragment = fragmentIndex >= 0 ? location.slice(0, fragmentIndex) : location
    const queryIndex = beforeFragment.indexOf('?')
    const path = queryIndex >= 0 ? beforeFragment.slice(0, queryIndex) : beforeFragment
    const fragment = fragmentIndex >= 0 ? location.slice(fragmentIndex + 1) : ''
    return !hasInvalidEscape(path) && !hasInvalidEscape(fragment)
  }

  const firstColon = location.indexOf(':')
  if (firstColon >= 0 && location.slice(firstColon + 1).startsWith('-')) return false
  return v4GitScpRE.test(location)
}

function isMarketplaceURL(location: string): boolean {
  const value = trimGoWhitespace(location)
  if (/[\u0000-\u001f\u007f]/.test(value) || value.includes('\\') || hasInvalidEscape(value)) return false

  const match = value.match(/^https?:\/\/([^/?#]+)/i)
  if (!match) return false
  const authority = match[1]
  if (authority.includes(' ')) return false

  const hostPort = authority.slice(authority.lastIndexOf('@') + 1)
  if (hostPort === '' || !validGoHostCharacters(hostPort.replace(/%[0-9a-f]{2}/gi, 'x'))) return false

  if (hostPort.startsWith('[')) {
    const closingBracket = hostPort.lastIndexOf(']')
    return closingBracket > 1 && /^(:\d*)?$/.test(hostPort.slice(closingBracket + 1))
  }

  const lastColon = hostPort.lastIndexOf(':')
  return lastColon < 0 || (lastColon > 0 && /^:\d*$/.test(hostPort.slice(lastColon)))
}

export function validateSourceInput(source: SourceDraft): SourceFieldErrors {
  const errors: SourceFieldErrors = {}
  if (source.kind === 'directory') {
    if (source.location.trim() === '') errors.location = 'Enter a directory location.'
    return errors
  }
  if (source.kind === 'git') {
    if (!validateGitLocation(source.location)) errors.location = 'Enter a valid Git location.'
    return errors
  }
  if (!isMarketplaceURL(source.location)) errors.location = 'Enter an absolute HTTP(S) URL.'
  return errors
}
