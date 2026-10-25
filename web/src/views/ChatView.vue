<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { apiFetch, apiJSON, mergeToolCalls, readErrorMessage, readSSE, type ChatMessage, type Citation, type Conversation, type ToolCallSummary } from '../api'
import ToolCallList from '../components/ToolCallList.vue'

const route = useRoute()
const conversationId = Number(route.params.id)
const conversation = ref<Conversation | null>(null)
const messages = ref<ChatMessage[]>([])
const input = ref('')
const error = ref('')
const streaming = ref(false)
const liveVisible = ref(false)
const streamText = ref('')
const streamSkill = ref('')
const streamCitations = ref<Citation[]>([])
const streamToolCalls = ref<ToolCallSummary[]>([])
let controller: AbortController | null = null
let disposed = false

async function load(preserveError = false): Promise<boolean> {
  if (!preserveError) { error.value = '' }
  try {
    const data = await apiJSON<{ conversation: Conversation; messages: ChatMessage[] }>(`/api/conversations/${conversationId}`)
    conversation.value = data.conversation
    messages.value = data.messages.map((message) => ({ ...message, citations: message.citations ?? [], tool_calls: mergeToolCalls([], message.tool_calls) }))
    return true
  } catch {
    if (!error.value) { error.value = preserveError ? '刷新会话失败，已保留当前内容' : '会话不存在或无权访问' }
    return false
  }
}

async function send(): Promise<void> {
  const content = input.value.trim()
  if (content.length < 2 || streaming.value) { return }
  error.value = ''
  messages.value.push({ id: -Date.now(), conversation_id: conversationId, role: 'user', content, citations: [], skill_name: '', tool_calls: [], created_at: '' })
  input.value = ''
  streaming.value = true
  liveVisible.value = true
  streamText.value = ''
  streamSkill.value = ''
  streamCitations.value = []
  streamToolCalls.value = []
  controller = new AbortController()
  let doneID = 0
  let terminal = false
  try {
    const response = await apiFetch(`/api/conversations/${conversationId}/messages`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content }), signal: controller.signal,
    })
    if (!response.ok) { error.value = await readErrorMessage(response) }
    else if (!response.body) { error.value = '当前浏览器不支持流式读取' }
    else {
      for await (const event of readSSE(response.body)) {
        if (event.event === 'meta') {
          if ('skill' in event.data) { const skill = event.data.skill as { name?: string } | null; streamSkill.value = skill?.name ?? '' }
          if (Array.isArray(event.data.citations)) { streamCitations.value = event.data.citations as Citation[] }
          streamToolCalls.value = mergeToolCalls(streamToolCalls.value, event.data.tool_calls)
        } else if (event.event === 'delta') { streamText.value += String(event.data.text ?? '') }
        else if (event.event === 'done') { doneID = Number(event.data.message_id); terminal = true }
        else if (event.event === 'error') { error.value = '回答生成失败，请稍后重试'; terminal = true }
      }
      if (!terminal) { error.value = '连接中断，请稍后重试' }
    }
  } catch { if (!disposed) { error.value = '连接中断，请稍后重试' } }
  finally { streaming.value = false; controller = null }
  if (disposed) { return }
  const loaded = await load(true)
  if (loaded && doneID && messages.value.some((message) => message.id === doneID && message.role === 'assistant')) { liveVisible.value = false }
  else if (!streamText.value && streamToolCalls.value.length === 0) { liveVisible.value = false }
}

onMounted(() => load())
onUnmounted(() => { disposed = true; controller?.abort() })
</script>

<template>
  <p>
    <router-link to="/conversations">
      ← 返回会话
    </router-link>
  </p>
  <h1>{{ conversation ? conversation.title : '会话' }}</h1>
  <p
    v-if="error"
    class="error"
    role="alert"
  >
    {{ error }}
  </p>

  <div class="chat">
    <div
      v-for="message in messages"
      :key="message.id"
      class="card message"
      :class="message.role"
    >
      <strong>{{ message.role === 'user' ? '我' : '助手' }}</strong>
      <span
        v-if="message.skill_name"
        class="muted"
      > · 技能：{{ message.skill_name }}</span>
      <ToolCallList :calls="message.tool_calls ?? []" />
      <p style="white-space: pre-wrap">
        {{ message.content }}
      </p>
      <ul v-if="message.citations.length > 0">
        <li
          v-for="c in message.citations"
          :key="c.marker"
        >
          {{ c.marker }}
          <router-link :to="`/materials/${c.material_id}`">
            {{ c.material_title }}
          </router-link>
          <span class="muted"> · 切片 {{ c.chunk_index }}</span>
        </li>
      </ul>
    </div>

    <div
      v-if="liveVisible"
      class="card message assistant"
      data-test="live-answer"
    >
      <strong>助手</strong>
      <span
        v-if="streamSkill"
        class="muted"
      > · 技能：{{ streamSkill }}</span>
      <ToolCallList :calls="streamToolCalls" />
      <p
        class="answer-text"
        style="white-space: pre-wrap"
      >
        {{ streamText }}
      </p>
      <ul v-if="streamCitations.length > 0">
        <li
          v-for="c in streamCitations"
          :key="c.marker"
        >
          {{ c.marker }}
          <router-link :to="`/materials/${c.material_id}`">
            {{ c.material_title }}
          </router-link>
          <span class="muted"> · 切片 {{ c.chunk_index }}</span>
        </li>
      </ul>
      <p
        v-if="streaming && !streamText"
        class="muted"
      >
        正在思考…
      </p>
    </div>
  </div>

  <div class="card">
    <p>
      <input
        v-model="input"
        maxlength="500"
        placeholder="继续提问（2–500 字）"
        style="width: 75%"
        @keyup.enter="send"
      >
      <button
        :disabled="streaming"
        @click="send"
      >
        发送
      </button>
    </p>
  </div>
</template>
