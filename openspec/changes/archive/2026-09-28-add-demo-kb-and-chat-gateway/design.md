# Design

## Context

动机见 `proposal.md`。当前 `add-traceable-vector-retrieval` 已实现并可运行（Go + MySQL + Qdrant + Vue SPA + token 鉴权），但尚未归档，因此主规格 `openspec/specs/` 目前只有 `auth-upload`；本变更的演示语料、示例问句与网关复用行为以**新能力**承载，避免对尚未进入主规格的 `knowledge-retrieval` / `web-frontend` 做 MODIFIED。

观察到的现状：

- `store.Seed` 只写入两条占位材料（A/B 各一条，正文一句话），且 `cmd/api` 启动时**不触发**对种子材料的切片/向量入库；演示语料因此不可检索。
- `config.Load` 已读取 `EMBEDDING_*` 与 `CHAT_*`，但 `CHAT_BASE_URL` 默认空，`/api/ask` 在有依据时返回 503。
- SPA 的检索与问答界面没有示例问句。

## Goals / Non-Goals

**Goals:**

- 演示环境只配置一套网关（`EMBEDDING_*`）即可同时支持向量化与问答。
- 内置可检索、可问答、班级可区分的演示知识库，并在启动后自动完成索引。
- 经服务端提供示例问句并在界面上一键使用；含一条无依据样例。
- 演示样例可逐条复现。

**Non-Goals:**

- 不改动鉴权、班级隔离、检索算法与数据库表结构。
- 不引入多轮对话、流式输出或外部数据集。
- 不把演示语料做成可在线编辑的 CMS。

## Decisions

### 1. 对话网关回退到嵌入网关（同一 API）

`config.Load` 在读取后做回退：`CHAT_BASE_URL` 为空 → 取 `EMBEDDING_BASE_URL`；`CHAT_API_KEY` 为空 → 取 `EMBEDDING_API_KEY`；`CHAT_MODEL` 默认 `course-chat`。回退只发生在显式值缺失时，显式值始终优先。

- 备选：要求单独配置 `CHAT_*`（演示摩擦大）；引入统一 `GATEWAY_*` 变量（破坏既有 `EMBEDDING_*` 约定）。

### 2. 演示语料内置于种子（`store.Seed`）

在 `store.Seed` 增加 `demoSeeds`：A/B 两班各 3 篇（共 6 篇），主题围绕中小学教学与教研，正文充实（每篇 ≥ 3 段、足以切分为多个切片），并各自包含一个班级专属词（A：如「青竹」；B：如「红叶」）。标题保持可区分（含「A 班」/「B 班」）。按 `(class_id, title)` 幂等 upsert：材料已存在时不重复插入，正文存在时不重复插入。

- 备选：随包携带 Markdown 文件并在启动时读取（增加文件路径/容器挂载复杂度）；单独种子命令（不在首次启动自动生效，演示体验差）。

### 3. 启动时「确保索引」（best-effort，后台执行）

种子之后启动一个**后台**遍历：对每个「没有 ready 切片」的材料执行 `Indexer.IndexMaterial`。这样：

- 首次启动即把演示语料切片并写入 Qdrant；
- 嵌入网关暂时不可用时，启动不失败（切片置 pending/failed），下次启动或 `cmd/backfill` 可补齐；
- 放在后台 goroutine，避免阻塞 `/health` 与端口监听。

仅当材料**没有 ready 切片**时才处理，避免每次启动重复嵌入；已在同一文件提供 `cmd/backfill` 作为显式补嵌路径。

- 备选：在 `store.Seed` 内索引（store 依赖 indexer，层次倒置）；仅在手动命令中索引（演示需额外步骤）。

### 4. 示例问句经服务端接口提供

新增 `GET /api/examples`（需登录），返回服务端定义的示例问句列表，每项含问句文本与是否标记为无依据（`no_evidence`）。至少 3 条，其中 1 条无依据。问句使用**两班共有**的主题，保证在 A/B 班会话下都可回答；无依据问句用于演示「无依据不编造」。SPA 的检索与问答界面拉取并渲染为可点击项，点击即以该问句发起查询。

- 备选：在 SPA 中硬编码（非服务端提供、难与语料同步、难测试）。

### 5. 演示语料变更的处理

种子不覆盖已存在的正文；若需替换演示语料，删除数据卷重来，或对相关材料使用 `POST /api/materials/{id}/reindex`。文档写明。

## Risks / Trade-offs

- [首次启动时嵌入网关未配置] → 切片保持 pending/failed，启动不失败；下次启动或 `cmd/backfill` 补齐；README 说明。
- [启动后台索引反复重试失败材料] → 语料规模小（约 6 篇），可接受；失败会被记录，且 `cmd/backfill` 可显式执行。
- [演示语料被修改但库中已存在旧正文] → 种子不覆盖；文档给出重建/删除数据卷的做法。
- [示例问句与语料主题漂移] → 示例与语料同属本变更、共同评审；文档列出样例查询与预期以便回归。
- [示例接口泄露他班信息] → 示例问句为主题性问题，不包含他班正文；接口仍需登录。

## Migration Plan

1. 更新 `config.Load` 的对话网关回退，并更新 `.env.example`。
2. 扩展 `store.Seed` 的演示语料（幂等）。
3. 增加启动时「确保索引」后台遍历；保留 `cmd/backfill`。
4. 新增 `GET /api/examples`，并在 SPA 检索/问答界面展示。
5. 重启 Compose，执行样例查询，更新 README 的演示与网关说明。

回滚：删除新增演示材料或清空数据卷即可；配置回退不影响显式配置的部署。

## Open Questions

- 共享网关实际暴露的对话模型名（`CHAT_MODEL`）取值属部署配置，默认 `course-chat`，可用环境变量覆盖，不影响规约、方案与任务拆分。
