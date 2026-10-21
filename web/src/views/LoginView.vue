<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { apiJSON, setToken } from '../api'

const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')

async function submit(): Promise<void> {
  error.value = ''
  try {
    const data = await apiJSON<{ token: string }>('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: username.value, password: password.value }),
    })
    setToken(data.token)
    await router.push('/materials')
  } catch {
    error.value = '用户名或密码错误'
  }
}
</script>

<template>
  <div class="card" style="max-width: 360px; margin: 64px auto">
    <h1>CampusClaw 登录</h1>
    <form @submit.prevent="submit">
      <p>
        <label for="username">用户名</label><br />
        <input id="username" v-model="username" autocomplete="username" required />
      </p>
      <p>
        <label for="password">密码</label><br />
        <input id="password" v-model="password" type="password" autocomplete="current-password" required />
      </p>
      <button type="submit">登录</button>
    </form>
    <p v-if="error" class="error">{{ error }}</p>
  </div>
</template>
