export function releaseVersionLabel(version: string): string {
  return `v${version.replace(/^v+/i, '')}`
}
