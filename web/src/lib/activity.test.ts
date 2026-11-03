import { describe, expect, it } from 'vitest'
import { activityLabel, isToolCall } from './activity'

describe('isToolCall', () => {
  it('is true only for tools/call, including inside a batch', () => {
    expect(isToolCall('tools/call')).toBe(true)
    expect(isToolCall('tools/list,tools/call')).toBe(true)
    for (const m of ['initialize', 'notifications/initialized', 'tools/list', 'resources/list', 'prompts/list', 'GET', 'DELETE', 'tools/callx']) {
      expect(isToolCall(m)).toBe(false)
    }
  })
})

describe('activityLabel', () => {
  it('gives friendly labels to connection traffic', () => {
    expect(activityLabel('initialize')).toBe('Handshake')
    expect(activityLabel('notifications/initialized')).toBe('Connection ready')
    expect(activityLabel('tools/list')).toBe('Listed tools')
    expect(activityLabel('resources/list')).toBe('Listed resources')
    expect(activityLabel('prompts/list')).toBe('Listed prompts')
    expect(activityLabel('GET')).toBe('Event stream')
    expect(activityLabel('DELETE')).toBe('Disconnected')
    expect(activityLabel('tools/call')).toBe('Tool call')
  })

  it('labels each method in a batch and falls back to the raw method', () => {
    expect(activityLabel('tools/list,prompts/list')).toBe('Listed tools, Listed prompts')
    expect(activityLabel('custom/thing')).toBe('custom/thing')
  })
})
