export const INSTALL_COMMAND =
  'go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest'

export const GITHUB_REPOSITORY_URL = 'https://github.com/Adyen/agent-marketplace'
export const LATEST_RELEASE_URL = `${GITHUB_REPOSITORY_URL}/releases/latest`

// Documentation is a VitePress route below SITE_BASE; pass it through withBase.
export const DOCS_PATH = '/docs/'

export type GuiVisitorOS = 'macos' | 'linux' | 'other'

export type GuiDownloadLink = {
  label: string
  url: string
}

export type GuiDownloadSelection =
  | { status: 'available'; primary: GuiDownloadLink | null; links: GuiDownloadLink[] }
  | { status: 'fallback'; label: string; url: string }

type GitHubRelease = {
  tag_name: string
  assets: Array<{ name: string; browser_download_url: string }>
}

const LATEST_RELEASE_API = 'https://api.github.com/repos/Adyen/agent-marketplace/releases/latest'
const linuxPackages = [
  ['linux-amd64.rpm', 'RPM · amd64'],
  ['linux-amd64.deb', 'DEB · amd64'],
  ['linux-arm64.rpm', 'RPM · arm64'],
  ['linux-arm64.deb', 'DEB · arm64'],
] as const

export function fallbackGuiDownloads(): GuiDownloadSelection {
  return {
    status: 'fallback',
    label: 'View latest release on GitHub',
    url: LATEST_RELEASE_URL,
  }
}

export function detectGuiVisitorOS(platform: string, userAgent: string): GuiVisitorOS {
  const value = `${platform} ${userAgent}`.toLowerCase()
  if (/\b(android|iphone|ipad|ipod)\b/.test(value)) return 'other'
  if (value.includes('mac')) return 'macos'
  if (value.includes('linux')) return 'linux'
  return 'other'
}

export function selectGuiDownloads(
  release: GitHubRelease,
  visitorOS: GuiVisitorOS,
): GuiDownloadSelection {
  const assets = new Map<string, string>(
    release.assets.map(asset => [asset.name, asset.browser_download_url]),
  )
  const link = (suffix: string, label: string): GuiDownloadLink | undefined => {
    const url = assets.get(`agent-marketplace-gui-${release.tag_name}-${suffix}`)
    return url ? { label, url } : undefined
  }

  const dmg = link('macos-universal.dmg', 'macOS · Universal (.dmg)')
  const zip = link('macos-universal.zip', 'macOS update archive (.zip)')
  const packages = linuxPackages
    .map(([suffix, label]) => link(suffix, label))
    .filter((item): item is GuiDownloadLink => item !== undefined)
  const allLinks = [dmg, zip, ...packages].filter(
    (item): item is GuiDownloadLink => item !== undefined,
  )

  if (visitorOS === 'macos') {
    return dmg
      ? { status: 'available', primary: dmg, links: [] }
      : fallbackGuiDownloads()
  }

  const links = visitorOS === 'linux' ? packages : allLinks
  return links.length
    ? { status: 'available', primary: null, links }
    : fallbackGuiDownloads()
}

export async function loadGuiDownloads(visitorOS: GuiVisitorOS): Promise<GuiDownloadSelection> {
  try {
    const response = await fetch(LATEST_RELEASE_API, {
      headers: { Accept: 'application/vnd.github+json' },
    })
    if (!response.ok) return fallbackGuiDownloads()
    return selectGuiDownloads(await response.json() as GitHubRelease, visitorOS)
  } catch {
    return fallbackGuiDownloads()
  }
}
