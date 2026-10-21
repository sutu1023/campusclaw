<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  apiFetch,
  apiJSON,
  readErrorMessage,
  type Agent,
  type Material,
  type Skill,
} from '../api'

const role = ref('')
const agents = ref<Agent[]>([])
const skills = ref<Skill[]>([])
const materials = ref<Material[]>([])
const error = ref('')

const blank = { id: 0, name: '', prompt: '', enabled: true, skill_ids: [] as number[], material_ids: [] as number[] }
const form = ref({ ...blank, skill_ids: [] as number[], material_ids: [] as number[], id: 0 })

async function load(): Promise<void> {
  error.value = ''
  try {
    const me = await apiJSON<{ user: { role: string } }>('/api/me')
    role.value = me.user.role
    const a = await apiJSON<{ agents: Agent[] }>('/api/agents')
    agents.value = a.agents
    if (role.value === 'teacher') {
      const s = await apiJSON<{ skills: Skill[] }>('/api/skills')
      skills.value = s.skills
      const m = await apiJSON<{ materials: Material[] }>('/api/materials?page_size=100')
      materials.value = m.materials
    }
  } catch {
    error.value = '加载失败'
  }
}

function reset(): void {
  form.value = { ...blank, skill_ids: [], material_ids: [] }
}

function edit(agent: Agent): void {
  form.value = {
    id: agent.id,
    name: agent.name,
    prompt: agent.prompt,
    enabled: agent.enabled,
    skill_ids: [...agent.skill_ids],
    material_ids: [...agent.material_ids],
  }
}

function toggle(list: 'skill_ids' | 'material_ids', id: number): void {
  const current = form.value[list]
  form.value[list] = current.includes(id) ? current.filter((x) => x !== id) : [...current, id]
}

async function save(): Promise<void> {
  error.value = ''
  const path = form.value.id ? `/api/agents/${form.value.id}` : '/api/agents'
  const method = form.value.id ? 'PUT' : 'POST'
  const response = await apiFetch(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      name: form.value.name,
      prompt: form.value.prompt,
      enabled: form.value.enabled,
      skill_ids: form.value.skill_ids,
      material_ids: form.value.material_ids,
    }),
  })
  if (!response.ok) {
    error.value = await readErrorMessage(response)
    return
  }
  reset()
  await load()
}

onMounted(load)
</script>

<template>
  <h1>智能体</h1>
  <p v-if="error" class="error">{{ error }}</p>

  <table>
    <thead>
      <tr>
        <th>#</th>
        <th>名称</th>
        <th>技能</th>
        <th>材料</th>
        <th>启用</th>
        <th v-if="role === 'teacher'"></th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="agent in agents" :key="agent.id">
        <td>{{ agent.id }}</td>
        <td>{{ agent.name }}</td>
        <td>{{ agent.skill_ids.length }}</td>
        <td>{{ agent.material_ids.length }}</td>
        <td>{{ agent.enabled ? '是' : '否' }}</td>
        <td v-if="role === 'teacher'">
          <button class="secondary" @click="edit(agent)">编辑</button>
        </td>
      </tr>
    </tbody>
  </table>
  <p v-if="agents.length === 0" class="muted">本班暂无智能体。</p>

  <div v-if="role === 'teacher'" class="card">
    <h2>{{ form.id ? `编辑智能体 #${form.id}` : '新建智能体' }}</h2>
    <p><input v-model="form.name" placeholder="名称（不超过 64 字）" /></p>
    <p><textarea v-model="form.prompt" rows="3" placeholder="角色提示词（留空则使用默认角色）" style="width: 100%"></textarea></p>
    <p><label><input v-model="form.enabled" type="checkbox" /> 启用</label></p>
    <h3>绑定技能</h3>
    <p>
      <label v-for="skill in skills" :key="skill.id" class="inline">
        <input
          type="checkbox"
          :checked="form.skill_ids.includes(skill.id)"
          @change="toggle('skill_ids', skill.id)"
        />
        {{ skill.name }}
      </label>
      <span v-if="skills.length === 0" class="muted">本班暂无技能。</span>
    </p>
    <h3>绑定材料</h3>
    <p>
      <label v-for="material in materials" :key="material.id" class="inline">
        <input
          type="checkbox"
          :checked="form.material_ids.includes(material.id)"
          @change="toggle('material_ids', material.id)"
        />
        {{ material.title }}
      </label>
      <span v-if="materials.length === 0" class="muted">本班暂无材料。</span>
    </p>
    <button @click="save">{{ form.id ? '保存' : '创建' }}</button>
    <button v-if="form.id" class="secondary" @click="reset">取消</button>
  </div>
</template>
