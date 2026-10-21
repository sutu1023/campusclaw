<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { apiJSON } from '../api'

interface Citation {
  marker: string
  chunk_id: number
  material_id: number
  material_title: string
  chunk_index: number
  start_offset: number
  end_offset: number
}

interface AskResult {
  answer: string
  status: string
  citations: Citation[]
}

interface Example {
  text: string
  no_evidence: boolean
}

const query = ref('')
const result = ref<AskResult | null>(null)
const error = ref('')
const examples = ref<Example[]>([])

async function loadExamples(): Promise<void> {
  try {
    const data = await apiJSON<{ examples: Example[] }>('/api/examples')
    examples.value = data.examples
  } catch {
    examples.value = []
  }
}

async function ask(): Promise<void> {
  error.value = ''
  result.value = null
  try {
    result.value = await apiJSON<AskResult>('/api/ask', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ q: query.value }),
    })
  } catch {
    error.value = '问答失败'
  }
}

function useExample(text: string): void {
  query.value = text
  void ask()
}

onMounted(loadExamples)
</script>

<template>
  <h1>知识库问答</h1>
  <div class="card">
    <p>
      <input v-model="query" placeholder="输入问题" style="width: 70%" @keyup.enter="ask" />
      <button @click="ask">提问</button>
    </p>
    <p v-if="examples.length > 0" class="examples">
      <span class="muted">示例问句：</span>
      <button
        v-for="example in examples"
        :key="example.text"
        type="button"
        class="secondary example"
        @click="useExample(example.text)"
      >
        {{ example.text }}
      </button>
    </p>
    <p v-if="error" class="error">{{ error }}</p>
  </div>

  <div v-if="result" class="card">
    <p style="white-space: pre-wrap">{{ result.answer }}</p>
    <div v-if="result.citations.length > 0">
      <h3>出处</h3>
      <ul>
        <li v-for="c in result.citations" :key="c.marker">
          {{ c.marker }}
          <router-link :to="`/materials/${c.material_id}`">{{ c.material_title }}</router-link>
          <span class="muted"> · 切片 {{ c.chunk_index }} · 字符 {{ c.start_offset }}–{{ c.end_offset }}</span>
        </li>
      </ul>
    </div>
  </div>
</template>
