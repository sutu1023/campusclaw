// API client for the CampusClaw SPA.
//
// The server issues an opaque bearer token at login. It is stored in
// localStorage, injected into the Authorization header on every request and
// never placed in the URL. A 401 clears the token and notifies the app so it
// can route back to the login page.

import { ref } from 'vue'

const TOKEN_KEY = 'campusclaw_token'
export const sessionRole = ref('')

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
  sessionRole.value = ''
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

export type ToolStatus = 'ok' | 'error' | 'denied'
export interface ToolCallSummary {
  call_id: string
  ref: string
  round: number
  status: ToolStatus
  text: string
}
export type McpState = 'disconnected' | 'connected' | 'pending_approval'
export interface McpServer {
  id: number
  class_id: number
  name: string
  base_url: string
  state: McpState
  has_token: boolean
  approved_hash?: string
  pending_hash?: string
  last_error?: string
  created_at: string
  updated_at: string
}
export interface McpTool {
  name: string
  description: string
  inputSchema: Record<string, unknown>
}
export interface McpChange {
  id: number
  server_id: number
  previous_hash: string
  snapshot_hash: string
  changed_tools: string[]
  created_at: string
  approved_by?: number
  approved_at?: string
}
export interface McpDirectory {
  server: McpServer
  tools: McpTool[]
  pending_tools?: McpTool[]
  approved_tools?: McpTool[]
  change?: McpChange
}
export interface McpAuditCall extends ToolCallSummary {
  id: number
  class_id: number
  server_id: number
  server_name: string
  user_id: number
  conversation_id: number
  question_message_id: number
  duration_ms: number
  created_at: string
  completed_at?: string
}
export interface McpAuditPage {
  calls: McpAuditCall[]
  total: number
  page: number
  page_size: number
}

export async function loadSessionRole(): Promise<string> {
  const token = getToken()
  if (!token) { sessionRole.value = ''; return '' }
  const data = await apiJSON<{ user: { role: string } }>('/api/me')
  if (getToken() !== token) { return '' }
  sessionRole.value = data.user.role
  return sessionRole.value
}

// Preserve first-arrival order within each round; repeated IDs update in place.
// Malformed metadata never becomes UI state or an executable message.
export function mergeToolCalls(current: ToolCallSummary[], incoming: unknown): ToolCallSummary[] {
  const merged = new Map(current.map((call) => [call.call_id, call]))
  if (!Array.isArray(incoming)) { return [...merged.values()] }
  for (const value of incoming) {
    if (!value || typeof value !== 'object') { continue }
    const call = value as Partial<ToolCallSummary>
    if (typeof call.call_id !== 'string' || !call.call_id || call.call_id.length > 128 || typeof call.ref !== 'string' || call.ref.length > 128 || typeof call.text !== 'string' || [...call.text].length > 500 || !Number.isInteger(call.round) || Number(call.round) < 0 || Number(call.round) > 64 || !['ok', 'error', 'denied'].includes(String(call.status))) { continue }
    merged.set(call.call_id, { call_id: call.call_id, ref: call.ref, round: Number(call.round), status: call.status as ToolStatus, text: call.text })
  }
  return [...merged.values()].sort((a, b) => a.round - b.round)
}

export interface ChatMessage {
  id: number
  conversation_id: number
  role: 'user' | 'assistant'
  content: string
  citations: Citation[]
  skill_name: string
  tool_calls?: ToolCallSummary[]
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
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) { break }
      // Normalize after joining: CR and LF may arrive in different chunks.
      buffer = (buffer + decoder.decode(value, { stream: true })).replace(/\r\n/g, '\n')
      let index: number
      while ((index = buffer.indexOf('\n\n')) >= 0) {
        const block = buffer.slice(0, index)
        buffer = buffer.slice(index + 2)
        const event = parseSseBlock(block)
        if (event) { yield event }
      }
    }
    buffer = (buffer + decoder.decode()).replace(/\r\n/g, '\n')
    const tail = parseSseBlock(buffer)
    if (tail) { yield tail }
  } finally { reader.releaseLock() }
}
