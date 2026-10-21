import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { apiJSON } = vi.hoisted(() => ({ apiJSON: vi.fn() }))
vi.mock('../api', () => ({ apiJSON }))

import SearchView from './SearchView.vue'

describe('SearchView example questions', () => {
  beforeEach(() => {
    apiJSON.mockReset()
    apiJSON.mockImplementation((path: string) => {
      if (path === '/api/examples') {
        return Promise.resolve({
          examples: [
            { text: '课堂提问有哪些有效技巧？', no_evidence: false },
            { text: '今天天气怎么样？', no_evidence: true },
          ],
        })
      }
      if (path === '/api/search') {
        return Promise.resolve({ hits: [], status: 'no_evidence', message: '资料中未找到相关内容' })
      }
      return Promise.resolve({})
    })
  })

  it('renders examples and runs a query with the clicked question', async () => {
    const wrapper = mount(SearchView, { global: { stubs: { RouterLink: true } } })
    await flushPromises()

    const buttons = wrapper.findAll('button.example')
    expect(buttons).toHaveLength(2)

    await buttons[0].trigger('click')
    await flushPromises()

    const searchCall = apiJSON.mock.calls.find((call) => call[0] === '/api/search')
    expect(searchCall).toBeTruthy()
    const body = JSON.parse(String(searchCall?.[1]?.body))
    expect(body.q).toBe('课堂提问有哪些有效技巧？')
  })
})
