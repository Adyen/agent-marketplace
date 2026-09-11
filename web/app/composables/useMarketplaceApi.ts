import { createMarketplaceApi, selectMarketplaceApiBase, selectMarketplaceFetcher, selectMarketplacePublicApiBase } from '~/lib/api'

export function useMarketplaceApi() {
  const config = useRuntimeConfig()
  // Compose supplies apiBaseInternal for SSR, so server rendering can call the
  // private API service directly. Browsers only receive config.public.apiBase
  // and therefore continue to use the public same-origin URL.
  // Nitro's dev proxy handles browser requests. SSR must retain the absolute
  // target because useRequestFetch resolves relative URLs inside Nuxt itself.
  const publicApiBase = selectMarketplacePublicApiBase(import.meta.client && import.meta.dev, config.public.apiBase)
  const internalApiBase = import.meta.server ? config.apiBaseInternal : undefined
  const apiBase = selectMarketplaceApiBase(import.meta.server, publicApiBase, internalApiBase)
  const requestFetcher = import.meta.server ? useRequestFetch() : $fetch
  const clientFetcher = import.meta.client ? $fetch.create({ baseURL: window.location.origin }) : $fetch
  const fetcher = selectMarketplaceFetcher(import.meta.server, apiBase, requestFetcher, clientFetcher)
  return createMarketplaceApi(fetcher, apiBase)
}
