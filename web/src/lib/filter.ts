import type { CatalogueItem, Kind } from '../api/types'

export interface CatalogueFilter {
  q: string
  kind: Kind | 'all'
  enabledOnly: boolean
}

function haystack(item: CatalogueItem): string {
  return [item.id, item.name, item.description, ...item.tags, ...item.tools.map((t) => t.name)]
    .join(' ')
    .toLowerCase()
}

/** Every whitespace-separated term must appear in id, name, description, tags or tool names. */
export function matchesQuery(item: CatalogueItem, q: string): boolean {
  const terms = q.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return true
  const text = haystack(item)
  return terms.every((t) => text.includes(t))
}

export function filterItems(items: CatalogueItem[], f: CatalogueFilter): CatalogueItem[] {
  return items.filter(
    (i) =>
      (f.kind === 'all' || i.kind === f.kind) && (!f.enabledOnly || i.state.enabled) && matchesQuery(i, f.q),
  )
}
