export function parseVersion(version) {
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.?(0|[1-9]\d*))?$/.exec(version)
  if (!match) throw Object.assign(new Error(`Invalid stable or RC docs version: ${version}`), { code: 'docs.invalid-version' })
  return match.slice(1).map((part) => part === undefined ? null : BigInt(part))
}

export function compareVersions(left, right) {
  const a = parseVersion(left)
  const b = parseVersion(right)
  for (let i = 0; i < a.length; i++) {
    if (a[i] === b[i]) continue
    if (a[i] === null) return -1
    if (b[i] === null) return 1
    return a[i] > b[i] ? -1 : 1
  }
  return left.localeCompare(right)
}

export function docsLocation(path, versions, base = '/') {
  path = path.split(/[?#]/)[0]
  if (!path.startsWith(base)) return null
  path = `/${path.slice(base.length)}`
  const target = [...versions].sort((a, b) => b.path.length - a.path.length)
    .find(({ path: prefix }) => path.startsWith(prefix) || path === prefix.slice(0, -1))
  if (!target) return null
  const page = path.slice(target.path.length).replace(/\.(html|md)$/, '').replace(/\/$/, '') || 'index'
  return { version: target.version, page }
}

export function switchVersion(path, version, versions, base = '/') {
  const target = versions.find((entry) => entry.version === version)
  if (!target) throw new Error(`Unknown docs version: ${version}`)
  const page = docsLocation(path, versions, base)?.page ?? 'index'
  const suffix = page !== 'index' && target.pages.includes(page) ? page : ''
  return `${base}${target.path.slice(1)}${suffix}`
}

export function searchVersionFilter(result) {
  const base = window.__VP_SITE_DATA__.base
  const scope = (path) => {
    const relative = path.slice(base.length)
    const match = /^docs\/(preview|\d+\.\d+\.\d+(?:-rc\.?\d+)?)(?:\/|[?#]|$)/.exec(relative)
    return match?.[1] ?? 'latest'
  }
  return scope(result.id) === scope(window.location.pathname)
}
