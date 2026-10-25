<script setup lang="ts">
import type { McpTool } from '../api'
defineProps<{ tools: McpTool[]; serverName: string }>()
</script>

<template>
  <p
    v-if="tools.length === 0"
    class="muted"
  >
    目录为空。
  </p>
  <article
    v-for="tool in tools"
    :key="tool.name"
    class="card"
  >
    <h3><code>{{ tool.name }}@{{ serverName }}</code></h3>
    <p class="tool-description">
      {{ tool.description }}
    </p>
    <p>完整输入结构：</p>
    <pre class="schema">{{ JSON.stringify(tool.inputSchema, null, 2) }}</pre>
  </article>
</template>

<style scoped>
.tool-description, .schema { white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
