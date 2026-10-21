<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { apiFetch, setToken } from './api'

const route = useRoute()
const router = useRouter()

async function logout(): Promise<void> {
  try {
    await apiFetch('/api/logout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    })
  } catch {
    // Even if the call fails, drop the local token.
  }
  setToken(null)
  await router.push('/login')
}
</script>

<template>
  <nav v-if="!route.meta.public" class="topbar">
    <span class="brand">CampusClaw</span>
    <router-link to="/materials">材料</router-link>
    <router-link to="/search">检索</router-link>
    <router-link to="/ask">问答</router-link>
    <router-link to="/agents">智能体</router-link>
    <router-link to="/skills">技能</router-link>
    <router-link to="/conversations">会话</router-link>
    <span class="spacer" />
    <button class="secondary" @click="logout">退出登录</button>
  </nav>
  <div class="container">
    <router-view />
  </div>
</template>
