import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  apiFetch,
  getToken,
  setToken,
  setUnauthorizedHandler,
  UnauthorizedError,
} from './api'

describe('api client', () => {
  beforeEach(() => {
    localStorage.clear()
    setUnauthorizedHandler(null)
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('injects the bearer token and never puts it in the URL', async () => {
    setToken('tok-123')
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/materials?page=1')

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(String(url)).not.toContain('tok-123')
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer tok-123')
  })

  it('clears the token and notifies the app on 401', async () => {
    setToken('tok-123')
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 401 })))

    await expect(apiFetch('/api/me')).rejects.toBeInstanceOf(UnauthorizedError)
    expect(getToken()).toBeNull()
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it('omits the Authorization header when there is no token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/health')

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(new Headers(init.headers).has('Authorization')).toBe(false)
  })
})
