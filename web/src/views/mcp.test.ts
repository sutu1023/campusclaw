import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import McpView from './McpView.vue'
import { setToken, setUnauthorizedHandler, type McpDirectory, type McpServer, type McpTool } from '../api'

let wrapper: VueWrapper | undefined
beforeEach(() => { localStorage.clear(); setToken('application-token'); setUnauthorizedHandler(null); vi.restoreAllMocks() })
afterEach(() => { wrapper?.unmount(); vi.unstubAllGlobals() })
const definition: McpTool = { name: 'formula_hint', description: '完整说明。'.repeat(180) + 'FULL_DESCRIPTION_END', inputSchema: { type: 'object', properties: { query: { type: 'string', maxLength: 500 } }, required: ['query'], additionalProperties: false } }
function mockBackend(role = 'teacher') {
  const server: McpServer = { id: 7, class_id: 1, name: 'campus-tools', base_url: 'http://mcp-demo:8090', state: 'disconnected', has_token: true, created_at: '2026-10-21', updated_at: '2026-10-21' }
  const state = { registered: true, pending: false, stale: false, connectError: false, total: 45, server }
  const requests: { url: string; options: RequestInit }[] = []
  const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })
  const fetchMock = vi.fn<typeof fetch>().mockImplementation(async (input, options = {}) => {
    const url = String(input); requests.push({ url, options })
    expect(new Headers(options.headers).get('Authorization')).toBe('Bearer application-token')
    expect(url.startsWith('/api/')).toBe(true)
    const method = options.method ?? 'GET'
    if (url === '/api/me') { return json({ user: { role } }) }
    if (url === '/api/mcp/servers' && method === 'GET') { return json({ servers: state.registered ? [server] : [] }) }
    if (url === '/api/mcp/servers' && method === 'POST') { state.registered = true; Object.assign(server, JSON.parse(String(options.body))); return json(server, 201) }
    if (url === '/api/mcp/servers/7' && method === 'PUT') { Object.assign(server, JSON.parse(String(options.body))); server.state = 'disconnected'; return json(server) }
    if (url === '/api/mcp/servers/7' && method === 'DELETE') { state.registered = false; return new Response(null, { status: 204 }) }
    if (url.endsWith('/connect')) {
      if (state.connectError) { server.state = 'disconnected'; return json({ error: '远端连接失败，请检查服务' }, 503) }
      server.state = state.pending ? 'pending_approval' : 'connected'; server.pending_hash = state.pending ? 'new-hash' : undefined
      return json(server)
    }
    if (url.endsWith('/disconnect')) { server.state = 'disconnected'; server.pending_hash = undefined; return new Response(null, { status: 204 }) }
    if (url.endsWith('/approve')) {
      if (state.stale) { server.state = 'disconnected'; return json({ error: '操作已失效，请刷新服务器状态后重试' }, 409) }
      expect(JSON.parse(String(options.body))).toEqual({ snapshot_hash: 'new-hash' })
      server.state = 'connected'; server.pending_hash = undefined; state.pending = false; return json(server)
    }
    if (url.endsWith('/tools')) {
      const directory: McpDirectory = { server: { ...server }, tools: server.state === 'connected' ? [definition] : [] }
      if (server.state === 'pending_approval') {
        directory.approved_tools = [definition]
        directory.pending_tools = [{ ...definition, description: 'NEW_COMPLETE_DESCRIPTION', inputSchema: { type: 'object', properties: { query: { type: 'string', minLength: 2 } } } }]
        directory.change = { id: 3, server_id: 7, previous_hash: 'old-hash', snapshot_hash: 'new-hash', changed_tools: ['formula_hint'], created_at: '2026-10-21T12:00:00Z' }
      }
      return json(directory)
    }
    if (url.startsWith('/api/mcp/audit?')) {
      const query = new URL(url, 'http://test').searchParams
      if (query.get('server_id') === '999') { return json({ error: 'not found' }, 404) }
      const statuses = query.get('status') ? [query.get('status')] : ['denied', 'error', 'ok']
      return json({ calls: statuses.map((status, index) => ({ id: 30 - index, call_id: `q17-c${index}`, ref: 'formula_hint@campus-tools', round: index, status, text: 'SAFE_AUDIT_TEXT', user_id: 11, conversation_id: 13, question_message_id: 17, duration_ms: 2, created_at: '2026-10-21T12:00:00Z' })), total: state.total, page: Number(query.get('page')), page_size: Number(query.get('page_size')) })
    }
    throw new Error(`unexpected request ${method} ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return { state, requests }
}
async function show() { wrapper = mount(McpView); await flushPromises(); return wrapper }
async function click(label: string) {
  const button = wrapper?.findAll('button').find((button) => button.text() === label)
  expect(button, label).toBeDefined()
  const form = button!.element.closest('form')
  if (button!.attributes('type') === 'submit' && form) {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  } else { await button!.trigger('click') }
  await flushPromises()
}

describe('MCP management', () => {
  it('shows has_token only, supports registration/update/delete and never stores MCP credentials', async () => {
    const { state, requests } = mockBackend(); state.registered = false
    await show()
    expect(wrapper!.find('input[type="password"]').exists()).toBe(false)
    await wrapper!.find('form').trigger('submit'); await flushPromises()
    expect(wrapper!.text()).toContain('已配置')
    const registered = requests.find((r) => r.options.method === 'POST' && r.url === '/api/mcp/servers')
    expect(JSON.parse(String(registered?.options.body))).toEqual({ name: 'campus-tools', base_url: 'http://mcp-demo:8090' })
    await click('修改')
    expect(wrapper!.get('[data-test="server-name"]').attributes('disabled')).toBeDefined()
    await wrapper!.get('[data-test="server-url"]').setValue('http://mcp-new:8090')
    await wrapper!.find('form').trigger('submit'); await flushPromises()
    expect(wrapper!.text()).toContain('http://mcp-new:8090')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await click('删除')
    expect(wrapper!.text()).toContain('本班暂无登记')
    expect(requests.some((r) => r.options.method === 'DELETE')).toBe(true)
    for (const request of requests) { expect(String(request.options.body ?? '')).not.toMatch(/token|credential|secret/i) }
    expect(localStorage.length).toBe(1)
    expect(localStorage.getItem('campusclaw_token')).toBe('application-token')
  })
  it('renders complete discovery and comparison, approves exactly the current hash, then disconnects', async () => {
    const { state } = mockBackend(); await show(); await click('连接 / 重连')
    expect(wrapper!.text()).toContain(definition.description)
    expect(wrapper!.find('pre').text()).toContain('additionalProperties')
    state.pending = true; await click('连接 / 重连')
    expect(wrapper!.text()).toContain('变更待确认')
    expect(wrapper!.text()).toContain('old-hash')
    expect(wrapper!.text()).toContain('new-hash')
    expect(wrapper!.text()).toContain('NEW_COMPLETE_DESCRIPTION')
    expect(wrapper!.text()).toContain(definition.description)
    expect(wrapper!.text()).not.toContain('当前可用工具')
    await click('确认当前快照')
    expect(wrapper!.text()).toContain('当前可用工具')
    await click('断开')
    expect(wrapper!.text()).toContain('当前不可调用')
    expect(wrapper!.text()).not.toContain('FULL_DESCRIPTION_END')
  })
  it('shows stale confirmation and failed connect errors without retaining usable definitions', async () => {
    const { state } = mockBackend(); state.pending = true; await show(); await click('连接 / 重连')
    state.stale = true; await click('确认当前快照')
    expect(wrapper!.get('[role="alert"]').text()).toContain('操作已失效')
    expect(wrapper!.text()).not.toContain('当前可用工具')
    state.pending = false; state.stale = false; await click('连接 / 重连')
    expect(wrapper!.text()).toContain('当前可用工具')
    state.connectError = true; await click('连接 / 重连')
    expect(wrapper!.get('[role="alert"]').text()).toContain('远端连接失败')
    expect(wrapper!.text()).not.toContain('FULL_DESCRIPTION_END')
  })
  it('filters and pages safe audit sources and displays server-side errors', async () => {
    const { requests } = mockBackend(); await show()
    expect(wrapper!.text()).toContain('用户 11 / 会话 13 / 提问 17')
    await click('下一页')
    expect(requests.at(-1)?.url).toContain('page=2')
    await wrapper!.get('[data-test="audit-server"]').setValue('7')
    await wrapper!.get('[data-test="audit-status"]').setValue('denied')
    await wrapper!.get('[data-test="audit-size"]').setValue('50')
    await click('筛选审计')
    const url = new URL(requests.at(-1)!.url, 'http://test')
    expect(url.searchParams.get('page')).toBe('1')
    expect(url.searchParams.get('server_id')).toBe('7')
    expect(url.searchParams.get('status')).toBe('denied')
    expect(url.searchParams.get('page_size')).toBe('50')
    await wrapper!.get('[data-test="audit-server"]').setValue('999'); await click('筛选审计')
    expect(wrapper!.get('[role="alert"]').text()).toBe('not found')
    expect(wrapper!.text()).not.toContain('SAFE_AUDIT_TEXT')
  })
  it('disables connect without has_token and never loads management data for a student', async () => {
    const { state } = mockBackend(); state.server.has_token = false; await show()
    expect(wrapper!.text()).toContain('未配置')
    expect(wrapper!.findAll('button').find((b) => b.text() === '连接 / 重连')!.attributes('disabled')).toBeDefined()
    wrapper!.unmount(); const { requests } = mockBackend('student'); await show()
    expect(wrapper!.text()).toContain('仅教师可管理')
    expect(wrapper!.find('form').exists()).toBe(false)
    expect(requests).toHaveLength(1)
  })
})
