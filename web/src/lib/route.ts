/** Minimal history-based routing: the daemon serves index.html for unknown paths (internal/webui). */
export type Tab = 'catalogue' | 'activity' | 'publish'

const paths: Record<Tab, string> = { catalogue: '/', activity: '/activity', publish: '/publish' }
const titles: Record<Tab, string> = { catalogue: 'Catalogue', activity: 'Activity', publish: 'Publish your server' }

export const pathForTab = (tab: Tab): string => paths[tab]

/** Document title for a view, so screen readers and history entries name the page. */
export const viewTitle = (tab: Tab): string => `${titles[tab]} · mcp-hub`

/** Id of each view's heading (tabindex=-1), which receives focus after client-side navigation. */
export const viewHeadingId = (tab: Tab): string => `${tab}-heading`

export function tabFromPath(path: string): Tab {
  const hit = (Object.keys(paths) as Tab[]).find((t) => paths[t] === path)
  return hit ?? 'catalogue'
}

const forward = { mcpHubForward: true }

/** True for the synthetic popstate fired by followLink (a forward navigation, not Back/Forward). */
export const isForwardNav = (e: PopStateEvent): boolean =>
  (e.state as typeof forward | null)?.mcpHubForward === true

/**
 * Handles a click on an in-app <a href>: plain clicks navigate client-side (App listens for popstate);
 * modified clicks (new tab/window) fall through to the browser.
 */
export function followLink(e: MouseEvent, path: string): void {
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return
  e.preventDefault()
  if (location.pathname !== path) history.pushState(null, '', path)
  window.dispatchEvent(new PopStateEvent('popstate', { state: forward }))
}
