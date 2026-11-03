import { afterEach, describe, expect, it, vi } from 'vitest'
import { followLink, pathForTab, tabFromPath } from './route'

describe('route', () => {
  afterEach(() => history.replaceState(null, '', '/'))

  it('maps paths to tabs and back', () => {
    expect(tabFromPath('/publish')).toBe('publish')
    expect(tabFromPath('/activity')).toBe('activity')
    expect(tabFromPath('/')).toBe('catalogue')
    expect(tabFromPath('/nope')).toBe('catalogue')
    expect(pathForTab('publish')).toBe('/publish')
    expect(pathForTab('activity')).toBe('/activity')
    expect(pathForTab('catalogue')).toBe('/')
  })

  it('followLink pushes the path and fires popstate for plain clicks only', () => {
    const onPop = vi.fn()
    window.addEventListener('popstate', onPop)
    const plain = new MouseEvent('click', { cancelable: true })
    followLink(plain, '/publish')
    expect(plain.defaultPrevented).toBe(true)
    expect(location.pathname).toBe('/publish')
    expect(onPop).toHaveBeenCalledTimes(1)

    history.replaceState(null, '', '/')
    const modified = new MouseEvent('click', { cancelable: true, metaKey: true })
    followLink(modified, '/publish')
    expect(modified.defaultPrevented).toBe(false)
    expect(location.pathname).toBe('/')
    expect(onPop).toHaveBeenCalledTimes(1)
    window.removeEventListener('popstate', onPop)
  })
})
