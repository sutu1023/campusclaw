// API client for the CampusClaw SPA.
//
// The server issues an opaque bearer token at login. It is stored in
// localStorage, injected into the Authorization header on every request and
// never placed in the URL. A 401 clears the token and notifies the app so it
// can route back to the login page.

const TOKEN_KEY = 'campusclaw_token'

export class UnauthorizedError extends Error {
  constructor() {
    super('unauthorized')
    this.name = 'UnauthorizedError'
  }
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string | null): void {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token)
  } else {
    localStorage.removeItem(TOKEN_KEY)
  }
}

let onUnauthorized: (() => void) | null = null

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler
}

export async function apiFetch(path: string, options: RequestInit = {}): Promise<Response> {
  const headers = new Headers(options.headers ?? {})
  const token = getToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(path, { ...options, headers })
  if (response.status === 401) {
    setToken(null)
    if (onUnauthorized) {
      onUnauthorized()
    }
    throw new UnauthorizedError()
  }
  return response
}

export async function apiJSON<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await apiFetch(path, options)
  if (!response.ok) {
    throw new Error(`request failed with status ${response.status}`)
  }
  return (await response.json()) as T
}

// readErrorMessage extracts the human-readable error a handler returned.
export async function readErrorMessage(response: Response): Promise<string> {
  try {
    const data = (await response.json()) as { error?: string }
    if (data && typeof data.error === 'string' && data.error) {
      return data.error
    }
  } catch {
    // fall through to the generic message
  }
  return `请求失败（${response.status}）`
}

// --- Lesson 5 domain types -------------------------------------------------

export interface Agent {
  id: number
  name: string
  prompt: string
  enabled: boolean
  skill_ids: number[]
  material_ids: number[]
}

export interface Skill {
  id: number
  name: string
  trigger: string
  description: string
  tools: string[]
  source: string
  subject: string
  enabled: boolean
}

export interface Conversation {
  id: number
  class_id: number
  user_id: number
  agent_id: number
  title: string
  created_at: string
  updated_at: string
}

export interface Citation {
  marker: string
  chunk_id: number
  material_id: number
  material_title: string
  chunk_index: number
  start_offset: number
  end_offset: number
}

export interface ChatMessage {
  id: number
  conversation_id: number
  role: 'user' | 'assistant'
  content: string
  citations: Citation[]
  skill_name: string
  created_at: string
}

export interface Material {
  id: number
  title: string
}

// --- SSE decoding ----------------------------------------------------------

export interface SseEvent {
  event: string
  data: Record<string, unknown>
}

function parseSseBlock(block: string): SseEvent | null {
  let event = ''
  let data = ''
  for (const rawLine of block.split('\n')) {
    const line = rawLine.replace(/\r$/, '')
    if (line.startsWith('event:')) {
      event = line.slice('event:'.length).trim()
    } else if (line.startsWith('data:')) {
      data += line.slice('data:'.length).trim()
    }
  }
  if (!event) {
    return null
  }
  let parsed: Record<string, unknown>
  try {
    parsed = JSON.parse(data) as Record<string, unknown>
  } catch {
    parsed = { raw: data }
  }
  return { event, data: parsed }
}

// readSSE decodes a text/event-stream body into events, buffering partial
// frames that span chunk boundaries.
export async function* readSSE(stream: ReadableStream<Uint8Array>): AsyncGenerator<SseEvent> {
  const reader = stream.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done) {
      break
    }
    buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')
    let index: number
    while ((index = buffer.indexOf('\n\n')) >= 0) {
      const block = buffer.slice(0, index)
      buffer = buffer.slice(index + 2)
      const event = parseSseBlock(block)
      if (event) {
        yield event
      }
    }
  }
  buffer += decoder.decode().replace(/\r\n/g, '\n')
  const tail = parseSseBlock(buffer)
  if (tail) {
    yield tail
  }
}
