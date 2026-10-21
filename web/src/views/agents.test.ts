import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  apiJSON: vi.fn(),
  apiFetch: vi.fn(),
  readErrorMessage: vi.fn(async () => 'error'),
}))

vi.mock('../api', () => mocks)

import AgentsView from './AgentsView.vue'

function mockRole(role: string): void {
  mocks.apiJSON.mockImplementation(async (path: string) => {
    if (path === '/api/me') {
      return { user: { role } }
    }
    if (path === '/api/agents') {
      return { agents: [] }
    }
    if (path === '/api/skills') {
      return { skills: [] }
    }
    if (path.startsWith('/api/materials')) {
      return { materials: [] }
    }
    return {}
  })
}

describe('AgentsView role rendering', () => {
  beforeEach(() => {
    mocks.apiJSON.mockReset()
    mocks.apiFetch.mockReset()
  })

  it('shows the create form to teachers', async () => {
    mockRole('teacher')
    const wrapper = mount(AgentsView)
    await flushPromises()
    expect(wrapper.text()).toContain('新建智能体')
  })

  it('hides the write entry from students', async () => {
    mockRole('student')
    const wrapper = mount(AgentsView)
    await flushPromises()
    expect(wrapper.text()).not.toContain('新建智能体')
  })
})
