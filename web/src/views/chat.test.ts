import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { setToken, setUnauthorizedHandler, type ChatMessage, type ToolCallSummary } from '../api'
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: '13' } }) }))
import ChatView from './ChatView.vue'

let wrapper: VueWrapper | undefined
beforeEach(() => { localStorage.clear(); setToken('application-token'); setUnauthorizedHandler(null) })
afterEach(async () => { wrapper?.unmount(); await flushPromises(); vi.unstubAllGlobals() })
const first: ToolCallSummary = { call_id: 'q1-c1', ref: 'formula_hint@campus-tools', round: 1, status: 'ok', text: '<img src=x onerror="window.attacked=true">安全提示' }
const second: ToolCallSummary = { call_id: 'q1-c2', ref: 'unit_convert@campus-tools', round: 2, status: 'error', text: '服务超时' }
const denied: ToolCallSummary = { call_id: 'q1-c3', ref: 'missing@campus-tools', round: 0, status: 'denied', text: '引用不可用' }
function message(id: number, role: 'user' | 'assistant', content: string, calls?: ToolCallSummary[]): ChatMessage {
  return { id, conversation_id: 13, role, content, citations: [], skill_name: role === 'assistant' ? '公式解题引导' : '', created_at: '', ...(calls ? { tool_calls: calls } : {}) }
}
function backend() {
  let sink: ReadableStreamDefaultController<Uint8Array>
  const encoder = new TextEncoder()
  const state = { messages: [] as ChatMessage[], signal: null as AbortSignal | null, gets: 0 }
  const fetchMock = vi.fn<typeof fetch>().mockImplementation(async (input, options = {}) => {
    expect(new Headers(options.headers).get('Authorization')).toBe('Bearer application-token')
    expect(String(input)).toMatch(/^\/api\/conversations\/13/)
    if ((options.method ?? 'GET') === 'GET') {
      state.gets++
      return new Response(JSON.stringify({ conversation: { id: 13, title: '本班会话' }, messages: state.messages }), { status: 200 })
    }
    state.signal = options.signal as AbortSignal
    const stream = new ReadableStream<Uint8Array>({ start(controller) { sink = controller } })
    state.signal.addEventListener('abort', () => sink.error(new DOMException('aborted', 'AbortError')), { once: true })
    return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
  })
  vi.stubGlobal('fetch', fetchMock)
  return {
    state,
    push: (text: string) => sink.enqueue(encoder.encode(text)),
    frame: (event: string, data: unknown) => sink.enqueue(encoder.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`)),
    close: () => sink.close(),
  }
}
async function show() { wrapper = mount(ChatView, { global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } }); await flushPromises() }
async function send() { await wrapper!.get('input').setValue('勾股公式怎么用'); await wrapper!.get('button').trigger('click'); await flushPromises() }

describe('chat tool state', () => {
  it('shows tool rounds before text, merges duplicate/framed metadata and restores escaped summaries on refresh', async () => {
    const remote = backend(); await show(); await send()
    const initial = `event: meta\r\ndata: ${JSON.stringify({ status: 'ok', skill: { name: '公式解题引导' }, citations: [{ marker: '[1]', material_id: 3, material_title: '<script>bad()</script>', chunk_index: 0 }] })}\r\n\r\n`
    const split = initial.indexOf('\r\n') + 1
    remote.push(initial.slice(0, split)); remote.push(initial.slice(split)); await flushPromises()
    remote.frame('meta', { tool_calls: [first] }); await flushPromises()
    expect(wrapper!.findAll('[data-call-id]')).toHaveLength(1)
    expect(wrapper!.get('.answer-text').text()).toBe('')
    expect(wrapper!.text()).toContain('成功（ok）')
    expect(wrapper!.text()).toContain('<img src=x')
    expect(wrapper!.find('img').exists()).toBe(false)
    expect(wrapper!.find('script').exists()).toBe(false)
    remote.frame('meta', { tool_calls: [first, second, denied] }); remote.frame('meta', { tool_calls: [second] }); await flushPromises()
    expect(wrapper!.findAll('[data-call-id]').map((node) => node.attributes('data-call-id'))).toEqual(['q1-c3', 'q1-c1', 'q1-c2'])
    expect(wrapper!.text()).toContain('技能：公式解题引导')
    expect(wrapper!.text()).toContain('<script>bad()</script>')
    remote.frame('delta', { text: '先看' }); await flushPromises(); expect(wrapper!.get('.answer-text').text()).toBe('先看')
    remote.frame('delta', { text: '条件' })
    remote.state.messages = [message(1, 'user', '勾股公式怎么用'), message(2, 'assistant', '先看条件', [first, second, denied])]
    remote.frame('done', { message_id: 2 }); remote.close(); await flushPromises()
    expect(wrapper!.find('[data-test="live-answer"]').exists()).toBe(false)
    expect(wrapper!.findAll('[data-call-id]')).toHaveLength(3)
    expect(wrapper!.text()).toContain('先看条件')
    wrapper!.unmount(); await show()
    expect(wrapper!.findAll('[data-call-id]')).toHaveLength(3)
    expect(wrapper!.text()).toContain('错误（error）')
    expect(wrapper!.text()).toContain('拒绝（denied）')
    expect(wrapper!.find('img').exists()).toBe(false)
  })
  it('retains partial text, safe tool states and a generic error even after message reload', async () => {
    const remote = backend(); await show(); await send()
    remote.frame('meta', { skill: { name: '公式解题引导' }, citations: [], tool_calls: [second] })
    remote.frame('delta', { text: '已收到的正文' }); remote.frame('error', { error: 'PRIVATE_UPSTREAM_DIAGNOSTIC' }); remote.close()
    remote.state.messages = [message(1, 'user', '勾股公式怎么用')]
    await flushPromises()
    expect(wrapper!.get('[role="alert"]').text()).toBe('回答生成失败，请稍后重试')
    expect(wrapper!.text()).not.toContain('PRIVATE_UPSTREAM_DIAGNOSTIC')
    expect(wrapper!.text()).toContain('已收到的正文')
    expect(wrapper!.text()).toContain('服务超时')
    expect(wrapper!.findAll('[data-call-id]')).toHaveLength(1)
    expect(remote.state.gets).toBe(2)
    expect(wrapper!.get('button').attributes('disabled')).toBeUndefined()
  })
  it('hides the toolbar for old/no-tool messages and ordinary streams', async () => {
    const remote = backend(); remote.state.messages = [message(8, 'assistant', '旧消息')]
    await show(); expect(wrapper!.find('.tool-calls').exists()).toBe(false)
    await send(); remote.frame('meta', { skill: null, citations: [] }); remote.frame('delta', { text: '普通回答' })
    remote.state.messages.push(message(9, 'user', '勾股公式怎么用'), message(10, 'assistant', '普通回答'))
    remote.frame('done', { message_id: 10 }); remote.close(); await flushPromises()
    expect(wrapper!.find('.tool-calls').exists()).toBe(false)
    expect(wrapper!.text()).toContain('普通回答')
  })
  it('aborts on navigation/unmount and does not start a refresh for a detached client', async () => {
    const remote = backend(); await show(); await send()
    wrapper!.unmount(); await flushPromises()
    expect(remote.state.signal?.aborted).toBe(true)
    expect(remote.state.gets).toBe(1)
  })
})
