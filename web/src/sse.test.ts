import { describe, expect, it } from 'vitest'
import { readSSE, type SseEvent } from './api'

function streamOf(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  let index = 0
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (index >= chunks.length) {
        controller.close()
        return
      }
      controller.enqueue(encoder.encode(chunks[index++]))
    },
  })
}

async function collect(stream: ReadableStream<Uint8Array>): Promise<SseEvent[]> {
  const events: SseEvent[] = []
  for await (const event of readSSE(stream)) {
    events.push(event)
  }
  return events
}

describe('readSSE', () => {
  it('decodes several frames in one chunk', async () => {
    const events = await collect(
      streamOf([
        'event: meta\ndata: {"status":"ok"}\n\n' +
          'event: delta\ndata: {"text":"a"}\n\nevent: delta\ndata: {"text":"b"}\n\n' +
          'event: done\ndata: {"message_id":7}\n\n',
      ]),
    )
    expect(events.map((e) => e.event)).toEqual(['meta', 'delta', 'delta', 'done'])
    expect(events[2].data.text).toBe('b')
    expect(events[3].data.message_id).toBe(7)
  })

  it('buffers frames split across chunks', async () => {
    const events = await collect(
      streamOf(['event: delta\nda', 'ta: {"text":"hel', 'lo"}\n\nevent: error\ndata: {"error":"x"}\n\n']),
    )
    expect(events).toEqual([
      { event: 'delta', data: { text: 'hello' } },
      { event: 'error', data: { error: 'x' } },
    ])
  })

  it('yields a final frame without a trailing blank line', async () => {
    const events = await collect(streamOf(['event: done\ndata: {"message_id":9}']))
    expect(events).toHaveLength(1)
    expect(events[0].data.message_id).toBe(9)
  })
})
