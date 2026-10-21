<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  apiFetch,
  apiJSON,
  readErrorMessage,
  readSSE,
  type ChatMessage,
  type Citation,
  type Conversation,
} from '../api'

const route = useRoute()
const conversationId = Number(route.params.id)
const conversation = ref<Conversation | null>(null)
const messages = ref<ChatMessage[]>([])
const input = ref('')
const error = ref('')
const streaming = ref(false)
const streamText = ref('')
const streamSkill = ref('')
const streamCitations = ref<Citation[]>([])

async function load(): Promise<void> {
  error.value = ''
  try {
    const data = await apiJSON<{ conversation: Conversation; messages: ChatMessage[] }>(
      `/api/conversations/${conversationId}`,
    )
    conversation.value = data.conversation
    messages.value = data.messages
  } catch {
    error.value = '会话不存在或无权访问'
  }
}

async function send(): Promise<void> {
  const content = input.value.trim()
  if (content.length < 2 || streaming.value) {
    return
  }
  error.value = ''
  messages.value.push({
    id: -Date.now(),
    conversation_id: conversationId,
    role: 'user',
    content,
    citations: [],
    skill_name: '',
    created_at: '',
  })
  input.value = ''
  streaming.value = true
  streamText.value = ''
  streamSkill.value = ''
  streamCitations.value = []

  try {
    const response = await apiFetch(`/api/conversations/${conversationId}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    })
    if (!response.ok) {
      error.value = await readErrorMessage(response)
      streaming.value = false
      await load()
      return
    }
    if (!response.body) {
      error.value = '当前浏览器不支持流式读取'
      streaming.value = false
      return
    }
    for await (const event of readSSE(response.body)) {
      if (event.event === 'meta') {
        const skill = event.data.skill as { name?: string } | null | undefined
        streamSkill.value = skill?.name ?? ''
        streamCitations.value = (event.data.citations as Citation[] | undefined) ?? []
      } else if (event.event === 'delta') {
        streamText.value += String(event.data.text ?? '')
      } else if (event.event === 'error') {
        error.value = String(event.data.error ?? '生成失败')
      }
    }
  } catch {
    error.value = '连接中断'
  }
  streaming.value = false
  await load()
}

onMounted(load)
</script>

<template>
  <p><router-link to="/conversations">← 返回会话</router-link></p>
  <h1>{{ conversation ? conversation.title : '会话' }}</h1>
  <p v-if="error" class="error">{{ error }}</p>

  <div class="chat">
    <div v-for="message in messages" :key="message.id" class="card message" :class="message.role">
      <strong>{{ message.role === 'user' ? '我' : '助手' }}</strong>
      <span v-if="message.skill_name" class="muted"> · 技能：{{ message.skill_name }}</span>
      <p style="white-space: pre-wrap">{{ message.content }}</p>
      <ul v-if="message.citations.length > 0">
        <li v-for="c in message.citations" :key="c.marker">
          {{ c.marker }}
          <router-link :to="`/materials/${c.material_id}`">{{ c.material_title }}</router-link>
          <span class="muted"> · 切片 {{ c.chunk_index }}</span>
        </li>
      </ul>
    </div>

    <div v-if="streaming" class="card message assistant">
      <strong>助手</strong>
      <span v-if="streamSkill" class="muted"> · 技能：{{ streamSkill }}</span>
      <p style="white-space: pre-wrap">{{ streamText }}</p>
      <ul v-if="streamCitations.length > 0">
        <li v-for="c in streamCitations" :key="c.marker">
          {{ c.marker }}
          <router-link :to="`/materials/${c.material_id}`">{{ c.material_title }}</router-link>
          <span class="muted"> · 切片 {{ c.chunk_index }}</span>
        </li>
      </ul>
      <p v-if="!streamText" class="muted">正在思考…</p>
    </div>
  </div>

  <div class="card">
    <p>
      <input v-model="input" placeholder="继续提问（2–500 字）" style="width: 75%" @keyup.enter="send" />
      <button :disabled="streaming" @click="send">发送</button>
    </p>
  </div>
</template>
