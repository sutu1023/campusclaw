import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({ apiJSON: vi.fn(), apiFetch: vi.fn(), readErrorMessage: vi.fn(async (response: Response) => (await response.json() as { error: string }).error) }))
vi.mock('../api', () => mocks)
import SkillsView from './SkillsView.vue'

let wrapper: VueWrapper | undefined
beforeEach(() => { mocks.apiJSON.mockReset(); mocks.apiFetch.mockReset() })
afterEach(() => { wrapper?.unmount() })
const source = '名称：公式解题引导\n适用条件：勾股\n资料：数学\n工具：knowledge_search,formula_hint@campus-tools\n启用：是\n\n只提示条件，不给得数。\n'
function backend(role = 'teacher') {
  const state = { skill: false, connected: true, saveError: '' }
  mocks.apiJSON.mockImplementation(async (path: string) => {
    if (path === '/api/me') { return { user: { role } } }
    if (path === '/api/skills') { return { skills: state.skill ? [{ id: 1, name: '公式解题引导', trigger: '勾股', subject: '数学', tools: ['knowledge_search', 'formula_hint@campus-tools'], enabled: true, source }] : [] } }
    if (path === '/api/mcp/servers') { return { servers: [{ id: 7, name: 'campus-tools', state: state.connected ? 'connected' : 'pending_approval' }, { id: 8, name: 'disconnected-tools', state: 'disconnected' }] } }
    if (path === '/api/mcp/servers/7/tools') { return { server: { id: 7, name: 'campus-tools', state: state.connected ? 'connected' : 'pending_approval' }, tools: state.connected ? [{ name: 'formula_hint', description: '完整公式条件说明', inputSchema: { type: 'object', properties: { query: { type: 'string' } } } }] : [] } }
    throw new Error(`unexpected path ${path}`)
  })
  mocks.apiFetch.mockImplementation(async (_path: string, options: RequestInit) => {
    expect(JSON.parse(String(options.body))).toEqual({ source })
    if (state.saveError) { return new Response(JSON.stringify({ error: state.saveError }), { status: 400 }) }
    state.skill = true
    return new Response('{}', { status: 201 })
  })
  return state
}
async function show() { wrapper = mount(SkillsView); await flushPromises(); return wrapper }
async function save() { await wrapper!.get('textarea').setValue(source); await wrapper!.findAll('button').find((b) => b.text() === '创建')!.trigger('click'); await flushPromises() }

describe('skill MCP references', () => {
  it('shows complete usable definitions, saves discovered references and reads the original source back', async () => {
    backend(); await show()
    expect(wrapper!.text()).toContain('tool@server')
    expect(wrapper!.text()).toContain('formula_hint@campus-tools')
    expect(wrapper!.text()).toContain('完整公式条件说明')
    expect(wrapper!.find('pre').text()).toContain('properties')
    expect(mocks.apiJSON).not.toHaveBeenCalledWith('/api/mcp/servers/8/tools')
    await save()
    expect(wrapper!.text()).toContain('已创建')
    await wrapper!.findAll('button').find((b) => b.text() === '编辑')!.trigger('click')
    expect((wrapper!.get('textarea').element as HTMLTextAreaElement).value).toBe(source)
  })
  it.each(['服务器未连接', '服务器正在等待变更确认'])('preserves source and displays backend save error: %s', async (reason) => {
    const state = backend(); state.saveError = `工具 formula_hint@campus-tools 不可保存：${reason}`
    await show(); await save()
    expect(wrapper!.text()).toContain(state.saveError)
    expect((wrapper!.get('textarea').element as HTMLTextAreaElement).value).toBe(source)
    state.connected = false
    await wrapper!.findAll('button').find((b) => b.text() === '刷新工具参考')!.trigger('click'); await flushPromises()
    expect(wrapper!.text()).not.toContain('完整公式条件说明')
    expect(wrapper!.text()).toContain('本班暂无可用 MCP 工具')
  })
  it('does not give students a write entry or query the teacher-only directory', async () => {
    backend('student'); await show()
    expect(wrapper!.find('textarea').exists()).toBe(false)
    expect(wrapper!.text()).not.toContain('新建技能')
    expect(mocks.apiJSON.mock.calls.some(([path]) => String(path).startsWith('/api/mcp'))).toBe(false)
    expect(mocks.apiFetch).not.toHaveBeenCalled()
  })
})
