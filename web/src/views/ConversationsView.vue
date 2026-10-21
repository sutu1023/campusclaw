<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { apiFetch, apiJSON, readErrorMessage, type Agent, type Conversation } from '../api'

const conversations = ref<Conversation[]>([])
const agents = ref<Agent[]>([])
const selected = ref(0)
const error = ref('')
const router = useRouter()

async function load(): Promise<void> {
  error.value = ''
  try {
    const data = await apiJSON<{ conversations: Conversation[] }>('/api/conversations')
    conversations.value = data.conversations
    const list = await apiJSON<{ agents: Agent[] }>('/api/agents')
    agents.value = list.agents.filter((agent) => agent.enabled)
    if (!selected.value && agents.value.length > 0) {
      selected.value = agents.value[0].id
    }
  } catch {
    error.value = '加载失败'
  }
}

async function create(): Promise<void> {
  error.value = ''
  if (!selected.value) {
    error.value = '本班暂无可用的智能体'
    return
  }
  const response = await apiFetch('/api/conversations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ agent_id: selected.value }),
  })
  if (!response.ok) {
    error.value = await readErrorMessage(response)
    return
  }
  const conversation = (await response.json()) as Conversation
  await router.push(`/chat/${conversation.id}`)
}

onMounted(load)
</script>

<template>
  <h1>我的会话</h1>
  <p v-if="error" class="error">{{ error }}</p>

  <div class="card">
    <h2>新建会话</h2>
    <p>
      <select v-model.number="selected">
        <option v-for="agent in agents" :key="agent.id" :value="agent.id">{{ agent.name }}</option>
      </select>
      <button :disabled="agents.length === 0" @click="create">新建并开始提问</button>
    </p>
    <p v-if="agents.length === 0" class="muted">本班暂无已启用的智能体，请联系教师。</p>
  </div>

  <table>
    <thead>
      <tr><th>#</th><th>标题</th><th>智能体</th><th>最近更新</th></tr>
    </thead>
    <tbody>
      <tr v-for="conversation in conversations" :key="conversation.id">
        <td>{{ conversation.id }}</td>
        <td><router-link :to="`/chat/${conversation.id}`">{{ conversation.title }}</router-link></td>
        <td>{{ conversation.agent_id }}</td>
        <td class="muted">{{ conversation.updated_at }}</td>
      </tr>
    </tbody>
  </table>
  <p v-if="conversations.length === 0" class="muted">还没有会话。</p>
</template>
