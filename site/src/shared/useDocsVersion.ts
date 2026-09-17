import { computed } from 'vue'
import { useData, useRoute } from 'vitepress'
import { docsLocation, switchVersion } from './docs-versions.mjs'

type DocsVersion = {
  version: string
  release?: string
  label: string
  path: string
  pages: string[]
}

export function useDocsVersion() {
  const { site, theme } = useData()
  const route = useRoute()
  const versions = computed<DocsVersion[]>(() => theme.value.docsVersions)
  const current = computed(() => {
    const location = docsLocation(route.path, versions.value, site.value.base)
    return versions.value.find((entry) => entry.version === location?.version)
  })
  const href = (version: string) => switchVersion(route.path, version, versions.value, site.value.base)
  return { versions, current, href }
}
