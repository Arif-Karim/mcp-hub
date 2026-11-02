import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SkillBanner from './SkillBanner.vue'
import { ApiError } from '../api/client'
import { createMockClient } from '../api/mock'
import type { ApiClient } from '../api/client'
import type { SkillStatus } from '../api/types'

const path = '/Users/dev/.claude/skills/mcp-hub/SKILL.md'
const skill = (installed: boolean, upToDate: boolean): SkillStatus => ({ installed, upToDate, path })

function setup(s: SkillStatus | undefined, client: ApiClient = createMockClient({ latencyMs: 0 })) {
  return mount(SkillBanner, { props: { skill: s, client } })
}

describe('SkillBanner', () => {
  it('not installed: explains why and offers Install skill', async () => {
    const client = createMockClient({ latencyMs: 0 })
    const install = vi.spyOn(client, 'installSkill')
    const w = setup(skill(false, false), client)
    expect(w.get('[data-test=skill-banner]').text()).toContain(
      "Claude Code skill not installed — Claude can't find or enable servers for you until it is.",
    )
    const btn = w.get('[data-test=skill-install]')
    expect(btn.text()).toBe('Install skill')

    await btn.trigger('click')
    expect(btn.text()).toBe('Installing…')
    expect(btn.attributes('disabled')).toBeDefined()
    await flushPromises()

    expect(install).toHaveBeenCalledOnce()
    expect(w.get('[data-test=skill-banner]').text()).toContain('Installed — restart Claude Code or run /reload-plugins')
    expect(w.find('[data-test=skill-install]').exists()).toBe(false)
    expect(w.emitted('installed')).toHaveLength(1)
  })

  it('installed but outdated: offers Update skill', () => {
    const w = setup(skill(true, false))
    expect(w.get('[data-test=skill-banner]').text()).toContain('out of date')
    expect(w.get('[data-test=skill-install]').text()).toBe('Update skill')
  })

  it('installed and up to date (or unknown): renders nothing', () => {
    expect(setup(skill(true, true)).find('[data-test=skill-banner]').exists()).toBe(false)
    expect(setup(undefined).find('[data-test=skill-banner]').exists()).toBe(false)
  })

  it('keeps showing the success message after the parent refreshes status', async () => {
    const w = setup(skill(false, false))
    await w.get('[data-test=skill-install]').trigger('click')
    await flushPromises()
    await w.setProps({ skill: skill(true, true) })
    expect(w.get('[data-test=skill-banner]').text()).toContain('Installed')
  })

  it('can be dismissed', async () => {
    const w = setup(skill(false, false))
    await w.get('[data-test=skill-dismiss]').trigger('click')
    expect(w.find('[data-test=skill-banner]').exists()).toBe(false)
  })

  it('shows a readable error and lets the user retry', async () => {
    const client = createMockClient({ latencyMs: 0 })
    vi.spyOn(client, 'installSkill').mockRejectedValueOnce(new ApiError(0, 'network', 'Failed to fetch'))
    const w = setup(skill(false, false), client)
    await w.get('[data-test=skill-install]').trigger('click')
    await flushPromises()
    expect(w.get('[role=alert]').text()).toContain("Can't reach the mcp-hub daemon")
    expect(w.get('[data-test=skill-install]').text()).toBe('Install skill')
    expect(w.emitted('installed')).toBeUndefined()
  })
})
