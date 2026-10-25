import { describe, expect, it } from 'vitest'
import { mergeToolCalls, type ToolCallSummary } from './api'

describe('safe tool summary merging', () => {
  it('deduplicates by call_id, preserves occurrence order per round and rejects invalid statuses', () => {
    const call: ToolCallSummary = { call_id: 'first', ref: 'hint@campus-tools', round: 1, status: 'error', text: 'failed' }
    const merged = mergeToolCalls([call], [{ ...call, text: 'updated' }, { ...call, call_id: 'second' }, { ...call, call_id: 'early', round: 0, status: 'denied' }, { ...call, call_id: 'invalid', status: 'running' }, { ...call, call_id: 'oversized', text: '字'.repeat(501) }])
    expect(merged.map((c) => c.call_id)).toEqual(['early', 'first', 'second'])
    expect(merged[1].text).toBe('updated')
    expect(mergeToolCalls(merged, undefined)).toEqual(merged)
  })
})
