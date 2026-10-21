<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { apiFetch, apiJSON } from '../api'

interface Material {
  id: number
  title: string
  created_at: string
}

const materials = ref<Material[]>([])
const role = ref('')
const error = ref('')
const title = ref('')
const file = ref<File | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)

async function load(): Promise<void> {
  error.value = ''
  try {
    const me = await apiJSON<{ user: { role: string; display_name: string } }>('/api/me')
    role.value = me.user.role
    const data = await apiJSON<{ materials: Material[] }>('/api/materials')
    materials.value = data.materials
  } catch {
    error.value = '加载失败'
  }
}

function onFile(event: Event): void {
  const target = event.target as HTMLInputElement
  file.value = target.files?.[0] ?? null
}

async function upload(): Promise<void> {
  if (!file.value) {
    return
  }
  error.value = ''
  const form = new FormData()
  form.append('file', file.value)
  if (title.value) {
    form.append('title', title.value)
  }
  const response = await apiFetch('/api/materials', { method: 'POST', body: form })
  if (!response.ok) {
    error.value = '上传失败'
    return
  }
  title.value = ''
  file.value = null
  if (fileInput.value) {
    fileInput.value.value = ''
  }
  await load()
}

onMounted(load)
</script>

<template>
  <h1>本班材料</h1>
  <p v-if="error" class="error">{{ error }}</p>

  <table>
    <thead>
      <tr><th>#</th><th>标题</th><th>上传时间</th></tr>
    </thead>
    <tbody>
      <tr v-for="material in materials" :key="material.id">
        <td>{{ material.id }}</td>
        <td>
          <router-link :to="`/materials/${material.id}`">{{ material.title }}</router-link>
        </td>
        <td class="muted">{{ material.created_at }}</td>
      </tr>
    </tbody>
  </table>
  <p v-if="materials.length === 0" class="muted">本班暂无材料。</p>

  <div v-if="role === 'teacher'" class="card">
    <h2>上传材料</h2>
    <p><input v-model="title" placeholder="标题（可选）" /></p>
    <p><input ref="fileInput" type="file" accept=".txt,.md" @change="onFile" /></p>
    <button @click="upload">上传</button>
  </div>
</template>
