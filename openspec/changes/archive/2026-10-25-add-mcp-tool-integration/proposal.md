# Proposal

## Why

第 5 课已具备班级隔离、技能路由、材料检索和流式对话，但技能的工具字段仍只展示、不实际执行。第 6 课需要在同仓部署真实 MCP 服务，并让教师可控地把它接入学生对话，形成发现、绑定、模型决策、鉴权执行与审计的闭环。

## What Changes

- 同仓新增 `mcp-server/`，由 Compose 构建为 `mcp-demo:8090`；提供受 Bearer 保护的 JSON-RPC `POST /mcp`、公开 `/health`，以及只给方法提示的 `formula_hint`、`unit_convert` 两个工具。
- 增加本班教师的 MCP 登记、修改地址、连接/发现、断开、删除、工具说明查看与调用审计。发现结果整体替换；说明/输入结构变更时暂停调用、记录变更并要求重新确认。
- 凭据原文仅由服务端环境变量提供；登记绑定部署方批准的班级/服务器/地址/凭据引用，接口只返回是否配置令牌，不让前端收集、保存或读取真实 MCP 密钥。
- 技能支持 `工具名@服务器名`，保留 `knowledge_search`；保存时校验本班归属、连接状态和发现清单，运行时再次授权。
- 从命中技能的 MCP 引用与发现快照构建模型工具声明；模型填写参数，宿主按同一输入结构校验后执行，工具结果作为不可信 `tool` 消息回传，可连续多轮。增加每问轮次/次数、会话累计次数、时间与体积上限，失败或超限后继续流式作答。
- 扩展 SSE `meta.tool_calls`、助手消息回放、教师管理页、Swagger 与接口测试；工具事件先于正文，明确区分 `ok`、`error`、`denied`。
- **行为变更**：替代第 5 课“工具字段只展示、禁止向模型提供工具/执行工具”的限制；保留词法技能路由、班级隔离、引用及流式契约。SSE 将支持一次回答中的多次 `meta` 更新，客户端须合并工具调用记录。

## Capabilities

### New Capabilities

- `mcp-server`: 同仓独立 HTTP JSON-RPC 服务、鉴权、两个教学提示工具及 Compose 真实互通。
- `mcp-management`: 班级隔离的教师管理、环境凭据绑定、握手发现、快照确认、失效与工具目录。
- `mcp-tool-execution`: 最小候选集、模型调用编排、逐调用授权与参数校验、安全边界、配额、失败降级及本班审计。

### Modified Capabilities

- `ai-skills`: 扩展工具引用语法和保存前校验；其他格式、权限、原文保留及路由规则不变。
- `model-gateway`: 支持工具声明、结构化调用与关联的工具消息，继续使用既有网关配置和最终答案真流式传输。
- `streaming-chat`: 明确工具数据边界，扩展多次 `meta` 与工具调用回放，替代“工具不执行”的限制。
- `web-frontend`: 新增教师 MCP 管理/审计入口和技能绑定辅助，聊天页合并展示工具状态。
- `api-docs`: 扩展注解生成的 MCP 管理、审计、技能验证及 SSE 契约与 Swagger 试调。

## Impact

- 后端：`internal/skill`、`internal/chat`、`internal/httpapi`、`internal/store`、`internal/config`、`cmd/api`；新增 MCP 协议客户端与编排边界。
- 数据：新增 MCP 服务器、发现/待确认快照、变更记录、调用审计与会话用量；扩容现有 `skills.tools_json VARCHAR(255)` 并为消息增加可回放的调用摘要，需要可重复的旧库迁移。
- 前端：`web/src/api.ts`、`router.ts`、`App.vue`、`SkillsView.vue`、`ChatView.vue` 及新增教师 MCP 页面。
- 部署/验证：`mcp-server/`、`docker-compose.yml`、`.env.example`、Docker 构建排除规则、`docs/`、`apitest/`、README 与 `VERIFICATION.md`；可增加一个离线 JSON Schema 校验依赖，不引入第二个仓库或消息队列。
- 不变：消息表仍只持久化 `user`/`assistant`，工具协议消息仅属于本轮可信宿主编排；现有 `/api/ask`、检索、历史窗口、智能体和材料归属不扩权。

### Scope and acceptance basis

依据[第 6 课任务清单](https://devops.hello1023.com/课程任务清单-第6课.html)及其链接的[课件](https://devops.hello1023.com/课件/第6课-课件-MCP与工具接入/index.html)，按用户确认采用课件完整功能范围，验收流程以本变更约定为准。冲突处以课件为准：参数由模型提出而不是宿主从问题硬编码；返回内容位于 `tool` 数据位而非直接提升为系统指令；调用可循环但必须受限。用确定性网关桩验证多轮和失败分支，同时必须用 Compose 中真实 MCP 服务完成端到端验收。

按用户决定，本变更不安排同伴交叉验收，该流程不计入任务总数，也不记作验收通过。跨班隔离、凭据不回传、断开拒绝和工具故障降级的功能要求及自动化验证仍保留；真实模型闭环和迁移/回滚演练仍须完成。

不包含写文件/发消息/数据外发等有副作用工具、任意第三方服务器市场、stdio/旧双通道 SSE、资源/提示能力、模型语义技能路由，以及第 8 课的指标、分布式追踪与最终回答外发意图扫描。
