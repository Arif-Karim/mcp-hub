import { describe, expect, it } from 'vitest'
import { fixture } from '../api/fixture'
import { filterItems, matchesQuery } from './filter'
import type { CatalogueItem } from '../api/types'

const items = (): CatalogueItem[] => structuredClone(fixture)
const ids = (xs: CatalogueItem[]) => xs.map((i) => i.id)

describe('filterItems', () => {
  it('returns everything with no filters', () => {
    expect(filterItems(items(), { q: '', kind: 'all', enabledOnly: false })).toHaveLength(5)
  })

  it('matches name, description, tags and tool names case-insensitively', () => {
    const all = items()
    expect(ids(filterItems(all, { q: 'deepwiki', kind: 'all', enabledOnly: false }))).toEqual(['deepwiki'])
    expect(ids(filterItems(all, { q: 'OWNING TEAM', kind: 'all', enabledOnly: false }))).toEqual([
      'service-directory',
    ])
    expect(ids(filterItems(all, { q: 'delivery', kind: 'all', enabledOnly: false }))).toEqual(['jira-rovo'])
    expect(ids(filterItems(all, { q: 'create_test_payment', kind: 'all', enabledOnly: false }))).toEqual([
      'payments-sandbox',
    ])
  })

  it('requires every whitespace-separated term to match', () => {
    expect(ids(filterItems(items(), { q: 'docs github', kind: 'all', enabledOnly: false }))).toEqual([
      'deepwiki',
    ])
  })

  it('filters by kind', () => {
    expect(ids(filterItems(items(), { q: '', kind: 'remote', enabledOnly: false }))).toEqual([
      'deepwiki',
      'context7',
      'jira-rovo',
    ])
  })

  it('filters to enabled only and combines with search and kind', () => {
    const all = items()
    all[1].state.enabled = true // deepwiki
    all[2].state.enabled = true // context7
    expect(ids(filterItems(all, { q: '', kind: 'all', enabledOnly: true }))).toEqual(['deepwiki', 'context7'])
    expect(ids(filterItems(all, { q: 'library', kind: 'remote', enabledOnly: true }))).toEqual(['context7'])
    expect(filterItems(all, { q: '', kind: 'local', enabledOnly: true })).toEqual([])
  })

  it('matchesQuery treats blank queries as a match', () => {
    expect(matchesQuery(items()[0], '   ')).toBe(true)
  })
})
