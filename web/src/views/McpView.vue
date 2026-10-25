<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { apiFetch, apiJSON, readErrorMessage, type McpAuditCall, type McpAuditPage, type McpDirectory, type McpServer } from '../api'
import McpToolList from '../components/McpToolList.vue'

const role = ref('')
const error = ref('')
const busy = ref(false)
const servers = ref<McpServer[]>([])
const directory = ref<McpDirectory | null>(null)
const editingId = ref(0)
const name = ref('campus-tools')
const baseURL = ref('http://mcp-demo:8090')
const audit = ref<McpAuditCall[]>([])
const auditServer = ref('')
const auditStatus = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

async function request<T>(path: string, options: NonNullable<Parameters<typeof apiFetch>[1]> = {}): Promise<T> {
  const response = await apiFetch(path, options)
  if (!response.ok) { throw new Error(await readErrorMessage(response)) }
  if (response.status === 204) { return undefined as T }
  return await response.json() as T
}
async function perform(action: () => Promise<void>): Promise<void> {
  if (busy.value) { return }
  busy.value = true
  error.value = ''
  try { await action() } catch (cause) { error.value = cause instanceof Error ? cause.message : '操作失败' }
  finally { busy.value = false }
}
async function loadServers(): Promise<void> {
  const data = await request<{ servers: McpServer[] }>('/api/mcp/servers')
  servers.value = data.servers
}
async function loadAudit(): Promise<void> {
  const query = new URLSearchParams({ page: String(page.value), page_size: String(pageSize.value) })
  if (auditServer.value) { query.set('server_id', auditServer.value) }
  if (auditStatus.value) { query.set('status', auditStatus.value) }
  audit.value = []
  total.value = 0
  const data = await request<McpAuditPage>(`/api/mcp/audit?${query}`)
  audit.value = data.calls
  total.value = data.total
  page.value = data.page
}
async function refresh(id?: number): Promise<void> {
  await loadServers()
  if (id && servers.value.some((server) => server.id === id)) {
    directory.value = await request<McpDirectory>(`/api/mcp/servers/${id}/tools`)
  }
  await loadAudit()
}
function reset(): void {
  editingId.value = 0
  name.value = 'campus-tools'
  baseURL.value = 'http://mcp-demo:8090'
}
function edit(server: McpServer): void {
  editingId.value = server.id
  name.value = server.name
  baseURL.value = server.base_url
}
async function save(): Promise<void> {
  await perform(async () => {
    const id = editingId.value
    if (directory.value?.server.id === id) { directory.value = null }
    await request(id ? `/api/mcp/servers/${id}` : '/api/mcp/servers', {
      method: id ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.value, base_url: baseURL.value }),
    })
    reset()
    await refresh(id)
  })
}
async function inspect(id: number): Promise<void> {
  await perform(async () => { directory.value = null; directory.value = await request<McpDirectory>(`/api/mcp/servers/${id}/tools`) })
}
async function action(id: number, operation: 'connect' | 'disconnect' | 'approve', hash?: string): Promise<void> {
  await perform(async () => {
    directory.value = null // never keep stale callable tools while mutating
    const response = await apiFetch(`/api/mcp/servers/${id}/${operation}`, {
      method: 'POST', ...(hash ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ snapshot_hash: hash }) } : {}),
    })
    if (!response.ok) {
      const reason = await readErrorMessage(response)
      try { await refresh(id) } catch { directory.value = null }
      throw new Error(reason)
    }
    await refresh(id)
  })
}
async function remove(server: McpServer): Promise<void> {
  if (!window.confirm(`删除 ${server.name}？名称将保留，不能分配给其他服务器；历史审计不会删除。`)) { return }
  await perform(async () => {
    await request(`/api/mcp/servers/${server.id}`, { method: 'DELETE' })
    if (directory.value?.server.id === server.id) { directory.value = null }
    if (editingId.value === server.id) { reset() }
    await refresh()
  })
}
async function filterAudit(): Promise<void> { await perform(async () => { page.value = 1; await loadAudit() }) }
async function movePage(change: number): Promise<void> { await perform(async () => { page.value += change; await loadAudit() }) }

onMounted(() => perform(async () => {
  const me = await apiJSON<{ user: { role: string } }>('/api/me')
  role.value = me.user.role
  if (role.value !== 'teacher') { error.value = '仅教师可管理本班 MCP 服务'; return }
  await refresh()
}))
</script>

<template>
  <h1>MCP 工具管理</h1>
  <p class="muted">
    仅接入部署方批准的本班服务。凭据由运维在服务端环境配置；浏览器不输入或保存 MCP 密钥。
  </p>
  <p
    v-if="error"
    role="alert"
    class="error"
  >
    {{ error }}
  </p>
  <template v-if="role === 'teacher'">
    <form
      class="card"
      @submit.prevent="save"
    >
      <h2>{{ editingId ? `修改服务器 #${editingId}` : '登记服务器' }}</h2>
      <p>
        <label>名称 <input
          v-model="name"
          data-test="server-name"
          :disabled="busy || !!editingId"
          required
          maxlength="32"
        ></label>
      </p>
      <p>
        <label>Base URL <input
          v-model="baseURL"
          data-test="server-url"
          :disabled="busy"
          required
          type="url"
        ></label>
      </p>
      <p class="muted">
        名称创建后不可修改；地址、班级和名称须精确匹配部署批准项。修改地址会断开并清空可用目录。
      </p>
      <button
        :disabled="busy"
        type="submit"
      >
        {{ editingId ? '保存地址' : '登记' }}
      </button>
      <button
        v-if="editingId"
        type="button"
        class="secondary"
        :disabled="busy"
        @click="reset"
      >
        取消
      </button>
    </form>

    <button
      class="secondary"
      :disabled="busy"
      @click="perform(() => refresh(directory?.server.id))"
    >
      刷新服务器与审计
    </button>
    <table>
      <thead><tr><th>编号 / 名称</th><th>地址</th><th>状态 / 服务端凭据</th><th>操作</th></tr></thead>
      <tbody>
        <tr
          v-for="server in servers"
          :key="server.id"
        >
          <td>#{{ server.id }} {{ server.name }}</td><td>{{ server.base_url }}</td>
          <td>
            {{ server.state }} · {{ server.has_token ? '已配置' : '未配置' }}<p
              v-if="server.last_error"
              class="error"
            >
              {{ server.last_error }}
            </p>
          </td>
          <td>
            <button
              class="secondary"
              :disabled="busy"
              @click="edit(server)"
            >
              修改
            </button>
            <button
              class="secondary"
              :disabled="busy || !server.has_token"
              @click="action(server.id, 'connect')"
            >
              连接 / 重连
            </button>
            <button
              class="secondary"
              :disabled="busy"
              @click="inspect(server.id)"
            >
              工具与变更
            </button>
            <button
              class="secondary"
              :disabled="busy"
              @click="action(server.id, 'disconnect')"
            >
              断开
            </button>
            <button
              class="secondary"
              :disabled="busy"
              @click="remove(server)"
            >
              删除
            </button>
          </td>
        </tr>
      </tbody>
    </table>
    <p
      v-if="servers.length === 0"
      class="muted"
    >
      本班暂无登记。
    </p>

    <section
      v-if="directory"
      class="card"
      data-test="directory"
    >
      <h2>{{ directory.server.name }} · {{ directory.server.state }}</h2>
      <template v-if="directory.server.state === 'connected'">
        <h3>当前可用工具</h3><McpToolList
          :tools="directory.tools"
          :server-name="directory.server.name"
        />
      </template>
      <p v-else>
        当前不可调用。断开不删除技能原文；需教师重新连接，不会自动重连。
      </p>
      <template v-if="directory.server.state === 'pending_approval'">
        <h3>变更待确认（确认前所有工具不可用）</h3>
        <p>上次批准：<code>{{ directory.change?.previous_hash }}</code></p>
        <p>待确认快照：<code>{{ directory.server.pending_hash }}</code></p>
        <p>变化工具：{{ directory.change?.changed_tools.join(', ') }} · {{ directory.change?.created_at }}</p>
        <h3>上次批准的完整定义（仅供比较，不可执行）</h3>
        <McpToolList
          :tools="directory.approved_tools ?? []"
          :server-name="directory.server.name"
        />
        <h3>新发现的完整定义（未批准）</h3>
        <McpToolList
          :tools="directory.pending_tools ?? []"
          :server-name="directory.server.name"
        />
        <button
          :disabled="busy || !directory.server.pending_hash"
          @click="action(directory.server.id, 'approve', directory.server.pending_hash)"
        >
          确认当前快照
        </button>
      </template>
    </section>

    <section class="card">
      <h2>本班调用审计</h2>
      <form @submit.prevent="filterAudit">
        <p>
          <label>服务器编号（可含本班已删除） <input
            v-model="auditServer"
            data-test="audit-server"
            type="number"
            min="1"
          ></label>
        </p>
        <p>
          <label>状态 <select
            v-model="auditStatus"
            data-test="audit-status"
          ><option value="">全部</option><option value="ok">ok</option><option value="error">error</option><option value="denied">denied</option></select></label>
          <label> 每页 <select
            v-model="pageSize"
            data-test="audit-size"
          ><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select></label>
        </p>
        <button
          :disabled="busy"
          type="submit"
        >
          筛选审计
        </button>
      </form>
      <table>
        <thead><tr><th>工具 / 状态</th><th>发起人 / 会话 / 提问</th><th>轮次 / 调用标识</th><th>时间 / 耗时</th><th>安全摘要</th></tr></thead>
        <tbody>
          <tr
            v-for="call in audit"
            :key="call.id"
          >
            <td>{{ call.ref }} · {{ call.status }}</td><td>用户 {{ call.user_id }} / 会话 {{ call.conversation_id }} / 提问 {{ call.question_message_id }}</td>
            <td>第 {{ call.round }} 轮 · {{ call.call_id }}</td><td>{{ call.created_at }} · {{ call.duration_ms }}ms</td><td>{{ call.text }}</td>
          </tr>
        </tbody>
      </table>
      <p
        v-if="audit.length === 0"
        class="muted"
      >
        当前筛选无记录。
      </p>
      <p>第 {{ page }} 页 · 共 {{ total }} 条（时间 / 编号倒序）</p>
      <button
        class="secondary"
        :disabled="busy || page <= 1"
        @click="movePage(-1)"
      >
        上一页
      </button>
      <button
        class="secondary"
        :disabled="busy || page * pageSize >= total"
        @click="movePage(1)"
      >
        下一页
      </button>
    </section>
  </template>
</template>
