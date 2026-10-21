<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { apiJSON } from '../api'

interface Material {
  id: number
  class_id: number
  title: string
  file_path?: string
  created_at: string
}

interface VectorChunk {
  chunk_id: number
  chunk_index: number
  start_offset: number
  end_offset: number
  index_status: string
  embedding_model?: string
  dimension: number
}

const props = defineProps<{ id: string }>()
const material = ref<Material | null>(null)
const body = ref('')
const chunks = ref<VectorChunk[]>([])
const error = ref('')

onMounted(async () => {
  try {
    const data = await apiJSON<{ material: Material; body_text: string }>(`/api/materials/${props.id}`)
    material.value = data.material
    body.value = data.body_text
    const vectors = await apiJSON<{ chunks: VectorChunk[] }>(`/api/materials/${props.id}/vectors`)
    chunks.value = vectors.chunks
  } catch {
    error.value = '加载失败或无权访问'
  }
})
</script>

<template>
  <p><router-link to="/materials">← 返回本班材料</router-link></p>
  <p v-if="error" class="error">{{ error }}</p>
  <template v-if="material">
    <h1>{{ material.title }}</h1>
    <ul class="muted">
      <li>材料 ID：{{ material.id }}</li>
      <li>班级 ID：{{ material.class_id }}</li>
      <li>上传时间：{{ material.created_at }}</li>
    </ul>

    <div class="card">
      <h2>原文</h2>
      <pre style="white-space: pre-wrap">{{ body }}</pre>
    </div>

    <div class="card">
      <h2>切片与向量</h2>
      <table>
        <thead>
          <tr><th>切片</th><th>字符区间</th><th>状态</th><th>模型</th><th>维度</th></tr>
        </thead>
        <tbody>
          <tr v-for="chunk in chunks" :key="chunk.chunk_id">
            <td>{{ chunk.chunk_index }}</td>
            <td>{{ chunk.start_offset }}–{{ chunk.end_offset }}</td>
            <td>{{ chunk.index_status }}</td>
            <td>{{ chunk.embedding_model }}</td>
            <td>{{ chunk.dimension }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </template>
</template>
