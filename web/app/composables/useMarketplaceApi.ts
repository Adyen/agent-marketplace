import { createMarketplaceApi, selectMarketplaceFetcher } from '~/lib/api'

export function useMarketplaceApi() {
  const config = useRuntimeConfig()
  // Nuxt's request-bound fetch forwards incoming cookies/headers during SSR for
  // relative same-origin requests. Absolute API bases remain external fetches.
  const requestFetcher = import.meta.server ? useRequestFetch() : $fetch
  const fetcher = selectMarketplaceFetcher(import.meta.server, config.public.apiBase, requestFetcher, $fetch)
  return createMarketplaceApi(fetcher, config.public.apiBase)
}
