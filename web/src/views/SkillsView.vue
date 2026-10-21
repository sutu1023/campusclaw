<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { apiFetch, apiJSON, readErrorMessage, type Skill } from '../api'

const role = ref('')
const skills = ref<Skill[]>([])
const error = ref('')
const message = ref('')
const source = ref('')
const editingId = ref(0)

async function load(): Promise<void> {
  error.value = ''
  try {
    const me = await apiJSON<{ user: { role: string } }>('/api/me')
    role.value = me.user.role
    const data = await apiJSON<{ skills: Skill[] }>('/api/skills')
    skills.value = data.skills
  } catch {
    error.value = '加载失败'
  }
}

function edit(skill: Skill): void {
  editingId.value = skill.id
  source.value = skill.source
  message.value = ''
}

function reset(): void {
  editingId.value = 0
  source.value = ''
}

async function save(): Promise<void> {
  error.value = ''
  message.value = ''
  const path = editingId.value ? `/api/skills/${editingId.value}` : '/api/skills'
  const method = editingId.value ? 'PUT' : 'POST'
  const response = await apiFetch(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ source: source.value }),
  })
  if (!response.ok) {
    error.value = await readErrorMessage(response)
    return
  }
  message.value = editingId.value ? '已保存' : '已创建'
  reset()
  await load()
}

async function toggleEnabled(skill: Skill): Promise<void> {
  error.value = ''
  const flipped = skill.source.replace(/^启用[:：].*$/m, `启用：${skill.enabled ? '否' : '是'}`)
  const response = await apiFetch(`/api/skills/${skill.id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ source: flipped }),
  })
  if (!response.ok) {
    error.value = await readErrorMessage(response)
    return
  }
  await load()
}

async function remove(skill: Skill): Promise<void> {
  error.value = ''
  const response = await apiFetch(`/api/skills/${skill.id}`, { method: 'DELETE' })
  if (!response.ok) {
    error.value = await readErrorMessage(response)
    return
  }
  await load()
}

onMounted(load)
</script>

<template>
  <h1>技能</h1>
  <p v-if="error" class="error">{{ error }}</p>
  <p v-if="message" class="muted">{{ message }}</p>

  <table>
    <thead>
      <tr>
        <th>#</th>
        <th>名称</th>
        <th>适用条件</th>
        <th>资料</th>
        <th>工具</th>
        <th>启用</th>
        <th v-if="role === 'teacher'"></th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="skill in skills" :key="skill.id">
        <td>{{ skill.id }}</td>
        <td>{{ skill.name }}</td>
        <td>{{ skill.trigger }}</td>
        <td>{{ skill.subject }}</td>
        <td>{{ skill.tools.join(', ') }}</td>
        <td>{{ skill.enabled ? '是' : '否' }}</td>
        <td v-if="role === 'teacher'">
          <button class="secondary" @click="edit(skill)">编辑</button>
          <button class="secondary" @click="toggleEnabled(skill)">
            {{ skill.enabled ? '停用' : '启用' }}
          </button>
          <button class="secondary" @click="remove(skill)">删除</button>
        </td>
      </tr>
    </tbody>
  </table>
  <p v-if="skills.length === 0" class="muted">本班暂无技能。</p>

  <div v-if="role === 'teacher'" class="card">
    <h2>{{ editingId ? `编辑技能 #${editingId}` : '新建技能' }}</h2>
    <p class="muted">
      空行以上写元数据（名称 / 适用条件 / 资料 / 工具 / 启用），空行以下写引导正文；工具只接受。
    </p>
    <p><textarea v-model="source" rows="10" style="width: 100%" placeholder="名称：数学解题引导&#10;适用条件：方程,去括号&#10;资料：数学&#10;工具：knowledge_search&#10;启用：是&#10;&#10;只指出下一步该检查什么，不写得数。"></textarea></p>
    <button @click="save">{{ editingId ? '保存' : '创建' }}</button>
    <button v-if="editingId" class="secondary" @click="reset">取消</button>
  </div>
</template>
