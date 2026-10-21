<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { apiJSON } from '../api'

interface Hit {
  chunk_id: number
  material_id: number
  material_title: string
  chunk_index: number
  start_offset: number
  end_offset: number
  snippet: string
}

interface SearchResult {
  hits: Hit[]
  status: string
  message?: string
}

interface Example {
  text: string
  no_evidence: boolean
}

const query = ref('')
const mode = ref<'keyword' | 'vector' | 'hybrid'>('hybrid')
const result = ref<SearchResult | null>(null)
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

async function search(): Promise<void> {
  error.value = ''
  result.value = null
  try {
    result.value = await apiJSON<SearchResult>('/api/search', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ q: query.value, mode: mode.value }),
    })
  } catch {
    error.value = '检索失败'
  }
}

function useExample(text: string): void {
  query.value = text
  void search()
}

onMounted(loadExamples)
</script>

<template>
  <h1>知识库检索</h1>
  <div class="card">
    <p>
      <input v-model="query" placeholder="输入查询内容" style="width: 60%" @keyup.enter="search" />
      <select v-model="mode">
        <option value="hybrid">混合</option>
        <option value="keyword">关键字</option>
        <option value="vector">向量</option>
      </select>
      <button @click="search">检索</button>
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

  <template v-if="result">
    <p v-if="result.hits.length === 0" class="muted">{{ result.message ?? '资料中未找到相关内容' }}</p>
    <div v-for="hit in result.hits" :key="hit.chunk_id" class="card">
      <p>
        <router-link :to="`/materials/${hit.material_id}`">{{ hit.material_title }}</router-link>
        <span class="muted"> · 切片 {{ hit.chunk_index }} · 字符 {{ hit.start_offset }}–{{ hit.end_offset }}</span>
      </p>
      <p>{{ hit.snippet }}</p>
    </div>
  </template>
</template>
