import { createMarketplaceApi, selectMarketplaceApiBase, selectMarketplaceFetcher } from '~/lib/api'

export function useMarketplaceApi() {
  const config = useRuntimeConfig()
  // Compose supplies apiBaseInternal for SSR, so server rendering can call the
  // private API service directly. Browsers only receive config.public.apiBase
  // and therefore continue to use the public same-origin URL.
  const apiBase = selectMarketplaceApiBase(import.meta.server, config.public.apiBase, config.apiBaseInternal)
  const requestFetcher = import.meta.server ? useRequestFetch() : $fetch
  const fetcher = selectMarketplaceFetcher(import.meta.server, apiBase, requestFetcher, $fetch)
  return createMarketplaceApi(fetcher, apiBase)
}
