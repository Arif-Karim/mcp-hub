import { httpClient } from './client'
import type { ApiClient } from './client'
import { createMockClient, demoFailuresFromUrl } from './mock'

/** The client the app uses: in-memory mock under `vite --mode mock`, real daemon otherwise. */
export const api: ApiClient =
  import.meta.env.VITE_API_MODE === 'mock'
    ? createMockClient({ liveTraffic: true, failures: demoFailuresFromUrl(window.location.search) })
    : httpClient
