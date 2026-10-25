<script setup lang="ts">
import type { ToolCallSummary } from '../api'
defineProps<{ calls: ToolCallSummary[] }>()
const labels = { ok: '成功', error: '错误', denied: '拒绝' }
</script>

<template>
  <section
    v-if="calls.length > 0"
    class="tool-calls"
    aria-label="工具使用状态"
  >
    <h3>工具使用</h3>
    <ul>
      <li
        v-for="call in calls"
        :key="call.call_id"
        :data-call-id="call.call_id"
      >
        <code>{{ call.ref }}</code> · 第 {{ call.round }} 轮 · {{ labels[call.status] }}（{{ call.status }}）
        <p>{{ call.text }}</p>
      </li>
    </ul>
  </section>
</template>
