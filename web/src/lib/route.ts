/** Minimal history-based routing: the daemon serves index.html for unknown paths (internal/webui). */
export type Tab = 'catalogue' | 'activity' | 'publish'

const paths: Record<Tab, string> = { catalogue: '/', activity: '/activity', publish: '/publish' }

export const pathForTab = (tab: Tab): string => paths[tab]

export function tabFromPath(path: string): Tab {
  const hit = (Object.keys(paths) as Tab[]).find((t) => paths[t] === path)
  return hit ?? 'catalogue'
}

/**
 * Handles a click on an in-app <a href>: plain clicks navigate client-side (App listens for popstate);
 * modified clicks (new tab/window) fall through to the browser.
 */
export function followLink(e: MouseEvent, path: string): void {
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return
  e.preventDefault()
  if (location.pathname !== path) history.pushState(null, '', path)
  window.dispatchEvent(new PopStateEvent('popstate'))
}
