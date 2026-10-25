import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import App from './App.vue'
import router from './router'
import { apiFetch, getToken, sessionRole, setToken, setUnauthorizedHandler } from './api'

let wrapper: VueWrapper | undefined
beforeEach(async () => {
  localStorage.clear()
  setToken(null)
  setUnauthorizedHandler(null)
  vi.unstubAllGlobals()
  await router.push('/login')
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.unstubAllGlobals() })
function roleFetch(role: string) {
  const mock = vi.fn<typeof fetch>().mockImplementation(async () => new Response(JSON.stringify({ user: { role } }), { status: 200 }))
  vi.stubGlobal('fetch', mock)
  return mock
}
async function showApp() {
  wrapper = mount(App, { global: { plugins: [router], stubs: { RouterView: true } } })
  await flushPromises()
  return wrapper
}

describe('MCP teacher-only entry', () => {
  it('shows teachers a link and accepts the guarded route with application Bearer', async () => {
    const fetchMock = roleFetch('teacher')
    setToken('application-login-token')
    await router.push('/materials')
    const app = await showApp()
    expect(app.find('a[href="/mcp"]').exists()).toBe(true)
    await router.push('/mcp')
    expect(router.currentRoute.value.name).toBe('mcp')
    for (const [, options] of fetchMock.mock.calls as unknown as [string, RequestInit][]) {
      expect(new Headers(options.headers).get('Authorization')).toBe('Bearer application-login-token')
    }
    expect(localStorage.length).toBe(1)
  })
  it('hides the student entry and refuses direct navigation without requesting management data', async () => {
    const fetchMock = roleFetch('student')
    setToken('student-login-token')
    await router.push('/materials')
    const app = await showApp()
    expect(app.find('a[href="/mcp"]').exists()).toBe(false)
    await router.push('/mcp')
    expect(router.currentRoute.value.path).toBe('/materials')
    expect(fetchMock.mock.calls.every(([url]) => !String(url).startsWith('/api/mcp'))).toBe(true)
  })
  it('clears role/token on 401 and redirects a guarded direct visit to login', async () => {
    setToken('expired-token')
    sessionRole.value = 'teacher'
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 401 })))
    await router.push('/mcp')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(getToken()).toBeNull()
    expect(sessionRole.value).toBe('')
  })
  it('does not treat an MCP upstream failure as application login expiry', async () => {
    setToken('valid-application-token')
    const unauthorized = vi.fn()
    setUnauthorizedHandler(unauthorized)
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"连接失败"}', { status: 503 })))
    const response = await apiFetch('/api/mcp/servers/1/connect', { method: 'POST' })
    expect(response.status).toBe(503)
    expect(getToken()).toBe('valid-application-token')
    expect(unauthorized).not.toHaveBeenCalled()
  })
})
