# Design

## Context

见 `proposal.md`。当前仓库（本变更前）已具备：token 认证与 RBAC、班级隔离（`auth-upload`）；材料切片与向量入库、关键字 / 向量 / 混合检索、单轮 `POST /api/ask`（`knowledge-retrieval`、`model-gateway`）；Vue 3 SPA 与北大红主题（`web-frontend`）。技术栈为 Go 标准库 `net/http`（`http.ServeMux`）、MySQL、Qdrant、外部嵌入 / 对话网关，前端 Vite 构建后由 Nginx 托管。

第 5 课在此之上增加「智能体 / 技能 / 会话」三组配置与状态、一次回答的编排与 SSE 流式输出。本设计说明这些行为落在哪些模块、依赖哪些表、以及关键取舍。

## Goals / Non-Goals

**Goals:**

- 六张新表与幂等建表 / 种子；把提示词、技能、材料绑定集中到智能体。
- 一个确定性、可单测的技能路由函数，与服务端拼装的两条消息。
- 基于标准库的 SSE 写出路径，含首事件前后不同的失败处理。
- 处理器注解生成的 OpenAPI 与可试调的 Swagger 页面。
- 前端新增配置与聊天界面，聊天流式解帧、逐段渲染。

**Non-Goals:**

- WebSocket、心跳、断线续传（单向场景，收益为零）。
- Agent Skills 规范的 `SKILL.md` / YAML frontmatter。
- 语义路由、交叉编码器重排序、模型自主选技能。
- 向模型注入真实工具（`knowledge_search` 仅为入库字段与展示值）。
- `class_prompts` 班级提示词表（回答路径只读智能体提示词）。
- 消息队列 / 分布式事务。

## Decisions

### 1. 数据模型：关联表而非绑定快照

新增 `tutor_agents(id, class_id, name, prompt, enabled)`、`skills(...)`、`conversations(...)`、`messages(...)`，以及两张纯关联表 `tutor_agent_skills(agent_id, skill_id)`、`tutor_agent_materials(agent_id, material_id)`。会话只锁 `agent_id`，绑定关系不随会话固化。

- **理由**：课件要求「教师改绑定后下一次提问即生效，不必重建智能体 / 新建会话」。关联表天然满足；若把 `skill_ids`/`material_ids` 存进智能体行（JSON 快照），要么每次提问读快照导致改绑定不生效，要么需额外合并逻辑。关联表还能用外键在删除技能时级联清除绑定。
- **备选**：智能体行内存 JSON 数组。被否：查询与级联复杂，且与「列表返回 `skill_ids`/`material_ids`」的接口形态需要额外反序列化。
- 班级边界与既有做法一致：所有查询带 `class_id`，按编号访问他班 / 未启用 / 不存在统一 404。表结构沿用现有 `schemaStatements` 追加，`CREATE TABLE IF NOT EXISTS` 幂等，重复启动不重建。

### 2. 技能文件解析：严格、可溯源

新增 `internal/skill` 解析器：以第一个空行切分元数据区与正文区；元数据区按行解析 `名称`、`适用条件`、`资料`、`工具`、`启用`，忽略 `#` 注释行与空行；出现重复字段、未定义字段、缺必填字段、正文为空、超长、工具不在白名单时返回结构化错误（面向教师的可读文案）。解析结果写 `skills` 各列，并原样保留 `source_text`。

- **理由**：课件把「文件是教师唯一的输入接口，列均可溯源到文件字段」作为核心，故必须保留原文；严格校验把错误挡在提交时。预置技能用 `seed_key` 做幂等键（`INSERT ... ON DUPLICATE KEY UPDATE`），教师自建技能 `seed_key` 为 `NULL`。
- **备选**：YAML frontmatter。被否：课件明确本课为自定义中文键格式，且考查点是「服务端路由」而非格式通用性。

### 3. 技能路由：纯函数

新增 `internal/routing`（或 `internal/skill.Route`）：输入候选技能列表与提问，输出至多一个技能。切词分隔符为中英文逗号、顿号、分号、竖线；两侧 `strings.ToLower`；`strings.Contains` 计分；取最高分，同分取 `id` 较小者，零分返回空。候选集由存储层一次查询给出（该智能体绑定且 `enabled=1`）。

- **理由**：课件强调确定性与可复现性（可单测、可在验证记录复现）。纯函数便于表驱动单测，也便于排查「哪个词没匹配上」。
- **备选**：模型自主选择或语义匹配。被否：不可复现、引入额外失败面。

### 4. 消息组装与提示注入边界

新增 `internal/chat` 之上的组装器，产出两条消息：

- **system**：固定五段拼接——智能体提示词（空则默认角色）、技能名称 + 说明（命中「解题引导」追加不写得数 / 选项 / 完整过程的硬性要求，未命中给「直接回答」提示）、有无资料的引用要求、不说明模型的约束、数据边界声明。资料正文**不在此**。
- **user**：编号资料 + 本轮提问。资料格式 `[n] 材料《标题》切片 k` + 正文，先剔除空正文与学科不符（技能 `subject_text` 过滤标题）再连续编号，每条截断 800 字。

资料编进用户消息、系统消息末尾追加边界声明、工具白名单不注入任何可调用工具，三者共同构成注入防线（对齐课件第 5 题）。

- **理由**：位置即信任边界。把不可信上传文本放得离 system 越远越好。
- **备选**：把资料放 system。被否：等于给上传文本发指令权限。

### 5. SSE 基于标准库

在 `httpapi` 新增流式处理器：设置 `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`Connection: keep-alive`、`X-Accel-Buffering: no`；用 `http.Flusher` 每帧 `Flush`；帧格式 `event: <name>\ndata: <json>\n\n`。用「是否已开始」标志：首个事件（`meta`）写出前可返回 401 / 400 / 404 / 503 的 JSON；写出后状态固定 200，失败只能发 `error` 事件。生成中途失败时：已有 `delta` 则以已生成内容收尾并落库，没有则向上返回错误。

- **理由**：单向推送 + 复用既有 Bearer 中间件与 Nginx 反代，SSE 成本最低。课件明确把分界线画在第一个事件。
- **工程细节**：Nginx 的 `/api/` location 与流式路径需 `proxy_buffering off`、`proxy_cache off`、`proxy_read_timeout 120s`；仅服务端写对不够，需全链路禁用缓冲。
- **备选**：WebSocket。被否：需自行实现升级、心跳、重连，单向场景无收益。

### 6. 对话网关流式消费（含非流式回退）

在 `internal/chat` 增加流式方法：以 `stream: true` 调用 OpenAI 兼容 `/chat/completions`，逐行解析 `data:` 增量并回调 `onDelta`。若网关不支持流（返回普通 JSON），则把整段内容作为**单个** `delta` 回调，保证功能可用。

- **理由**：课件要求降低首字延迟；不同网关对 `stream` 支持不一，回退保证演示环境可用。回退仍满足「`delta` 为增量、拼接等于全文」的契约。
- **备选**：只支持流式并在他处报错。被否：增加演示失败面。

### 7. OpenAPI：处理器注解 + Swagger UI

采用 `swaggo/swag` 注解：在处理器函数上方写 `@Summary`、`@Router`、`@Param`、`@Success`、`@Failure`、`@Security` 等；`swag init -g cmd/api/main.go` 生成 `docs/`（`swagger.json` 与 `docs.go`），构建时提交生成物。用 `docs.ReadDoc()` 提供 `GET /openapi.json`，用 `http-swagger` 的 stdlib Handler 提供 `GET /swagger/index.html`，并**在注册业务路由之前**挂载这两条路由。

- **理由**：课件要求「由处理器注解生成」，注解与处理器同源；注解里写实际路径，避免文档与实现漂移。
- **备选**：程序化构建 spec（如 `swaggest/openapi-go`）。被否：不满足「注解生成」的验收口径。备选：手写 `openapi.json`。被否：易与实现漂移，正是课件要避免的。
- **风险**：需要 `swag` 代码生成步骤（新增开发期依赖）与生成物提交。缓解：在 `Makefile` / 脚本中固化 `swag init`，CI / 本地构建前先执行，并在任务中显式列出。

### 8. 前端解帧与渲染

新增视图：智能体管理（教师可写）、技能管理（教师可写、展示原文）、会话列表 / 新建（选本班已启用智能体）、聊天页。聊天页用 `fetch` 发 POST（`EventSource` 只能 GET），读取 `response.body` 的 `ReadableStream`，按行解 `event:` / `data:` 帧并逐段追加。`meta` 到达即渲染技能与引用列表；引用材料先转义再渲染，禁止执行脚本。API 客户端 `apiFetch` 复用，401 仍清 token 跳登录。

- **理由**：与后端 SSE 帧格式一一对应，避免额外依赖。
- **备选**：引入 `eventsource-parser` 等库。被否：多依赖，解帧规则简单。

### 9. 接口自动化测试用 Python 标准库

`apitest/` 采用 Python 标准库 `urllib.request` / `json` / `http.client`，不引入第三方包（仓库 `requirements.txt` 已过期，不作为运行时依赖）。目录按 URL 组织（`cases/api/agents/POST.py` 等），`run_all.py` 收集脚本、执行并生成自包含 HTML 报告；报告输出到忽略目录（`.gitignore` 增加）。

- **理由**：课件要求「一接口一脚本、按 URL 组织、一键跑完并生成 HTML 报告」，且「报告不提交 Git」；标准库零安装门槛最低。
- **备选**：`pytest` + `requests`。被否：引入新依赖且与课件的脚本约定不符。
- 除状态码外，脚本通过 `GET /api/conversations/{id}`、`GET /api/agents` 等读回数据比对，落实「返回内容与数据库一致」。

## Risks / Trade-offs

- **SSE 被代理缓冲** → Nginx 显式 `proxy_buffering off` + 服务端 `X-Accel-Buffering: no` 两层防护，并在验收里用浏览器抓包确认多次 `delta`。
- **流开始后无法改状态码** → 把可提前判定的失败前置到检索之前；流内失败以 `error` 事件收尾，避免使用方按状态码分流时误判。
- **`swag` 代码生成与实现漂移** → 注解与处理器同函数，路径写实际注册路径；任务中固定生成步骤并在验收里打开 Swagger 实调。
- **短问句路由误命中 / 漏命中**（如适用条件写了高频虚词）→ 属课件已知取舍；路由为纯函数，可用单测与「技能开关」对比验收定位。
- **检索落空仍调用模型** → 成本换覆盖：与本班材料无关的通用请求仍可作答；课件明确此为主口径，`POST /api/ask` 保留固定文案口径。
- **无依据回答出现编号** → 系统消息要求 + 送达前剥除 `[n]` 两道措施叠加，不依赖模型每次都听话。
- **绑定表与删除竞态** → 删除技能 / 材料时在同一事务清理绑定，避免悬挂引用。

## Migration Plan

1. 追加六张表的幂等 DDL 与新种子（A 班数学 / 语文「解题引导」技能，带 `seed_key`）；对既有库执行 `Migrate` / `Seed` 不破坏既有数据。
2. 后端按「存储层 → 接口层 → 编排层 → 文档」落地；`/api/ask` 与 `/api/search` 行为不变，可随时独立回归。
3. 配置 Nginx（SSE）。前端随构建产物发布。
4. 回滚：新接口与前端的引用可整体移除；新表可保留（不影响旧路径），必要时按表名 `DROP`。
5. 验收：第 3 / 4 课既有场景回归 + 第 5 课 DoD（见 `streaming-chat`、`conversations` 等 spec 与 `任务清单`）。

## Open Questions

无。
