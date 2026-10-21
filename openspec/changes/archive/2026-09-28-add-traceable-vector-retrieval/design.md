# Design

## Context

动机见 `proposal.md`，可验收行为见 `specs/auth-upload/spec.md`（修改）与 `specs/knowledge-retrieval/spec.md`（新增）。

当前仓库的 `auth-upload` 以 Flask + SQLite + 客户端签名 Cookie 会话 + Jinja 模板实现，与第 3 课课件要求的 **Go + Nginx + MySQL + 服务端会话**不一致；第 4 课课件（`add-traceable-vector-retrieval` / `knowledge-retrieval`）要求在此基础上增加切分、向量化与三类检索。本变更同时按项目要求把前后端通信统一为 **token 方式**（会话状态仍在服务端，但以 `Authorization: Bearer` 不透明令牌替代 Cookie 传输），并把前端重构为 **Vue 3 + Vite + TypeScript 的独立 SPA**，统一采用**北大红**视觉主题。目标 API 契约与字段名已由课件与线上实例（`/week04/api`）双重确认，本文据此给出实现决策。仓库现有数据量很小：3 条 `materials`（2 条种子无正文、1 条上传）、1 条 `knowledge_entries`、3 个 bcrypt 用户。

## Goals / Non-Goals

**Goals:**

- 把 Web 层与数据库拉回课件基线（Go + Nginx + MySQL），并把鉴权统一为**服务端可撤销的 token 方式**（不透明 Bearer 令牌，`sessions` 表存哈希，替代 Cookie 会话）。
- 前端重构为 **Vue 3 + Vite + TypeScript SPA**，由 Nginx 托管静态资源并做前端路由 fallback，统一采用**北大红**主题。
- 上传后正文切片、向量入 Qdrant，支持 keyword / vector / hybrid 检索与带出处的问答。
- 班级隔离在两条检索路径与回表处均成立；无依据时不调用模型。
- 两库一致性以 MySQL 为事实来源，配可重复执行的重建路径。
- 用 `docker compose up` 一键启动 Nginx + Go + MySQL + Qdrant。

**Non-Goals:**

- 流式输出、多轮会话管理、交叉编码器重排序、消息队列/分布式事务、自部署模型、PDF/Word 解析（见 `proposal.md` Non-goals）。
- 不追求 MySQL 与 Qdrant 的强一致事务；以「MySQL 事实来源 + 幂等重建」替代。
- 不引入 JWT / 刷新令牌 / 第三方登录；令牌为服务端可撤销的不透明随机值。
- 不做服务端渲染（SSR）或同构渲染；前端为纯客户端 SPA。

## Decisions

### 1. 服务栈：Vue SPA + Go + Nginx + MySQL + Qdrant（替代 Flask + SQLite + Jinja）

三层容器：`web`（Nginx，托管 SPA 构建产物 + 反代 `/api` 到 Go）、`api`（Go）、`db`（MySQL）；`qdrant` 与两类模型网关仅由 `api` 在服务端访问。MySQL 不映射宿主机端口。前端为 **Vue 3 + Vite + TypeScript SPA**，构建产物作为静态资源由 Nginx 托管，并对前端路由做 history fallback；Go 服务只提供 `/api/*` 与 `/health` 的 JSON，不再渲染 HTML 模板。备选：保留 Flask/Jinja（与「SPA + token」要求不符）、React 18 + Vite（能力等价，本变更选 Vue 以贴合国内生态与课件习惯）。

### 2. Token 鉴权（服务端可撤销的不透明令牌）

- 登录成功由服务端生成 **256 bit 高熵随机令牌**，仅经响应体返回客户端；服务端 `sessions` 表只存 `token_hash = HMAC-SHA256(TOKEN_SECRET, token)`、`user_id`、`expires_at`、`created_at`，**不存明文令牌**。
- 客户端在 `Authorization: Bearer <token>` 头携带令牌，**不再使用 Cookie**。认证中间件每次请求计算 `token_hash`、查 `sessions` 行并读 `users` 得 `role`/`class_id` 写入上下文；过期行视为无效。
- **登录成功必须删除该用户旧会话并签发新令牌**（防会话固定）。登出删除会话行，令牌即时失效。
- 前端以 `localStorage` 保存令牌，SPA 启动时用 `/api/me` 校验；401 即清除并跳登录。令牌不出现在 URL / 查询串 / 日志中；认证响应统一 `Cache-Control: no-store`。
- 应用启动校验必需环境变量（含 `TOKEN_SECRET`）缺失即失败。
- 备选：JWT（无状态但登出不能立即失效，需短 TTL 或黑名单，本变更不采用）；Cookie 会话（不能带自定义头、需 CSRF 防护，且与「token 方式」要求不符）。
- 以 token 认证后不再需要 CSRF token；代价是 XSS 可窃取 `localStorage` 中的令牌，用严格 CSP 与输出转义缓解（见 Risks）。

### 3. 数据模型

```
classes(id, name)
users(id, username, display_name, password_hash, role, class_id)
sessions(token_hash, user_id, expires_at, created_at)
materials(id, class_id, title, file_path, uploader, created_at)
knowledge_entries(id, material_id, class_id, body_text, source, created_at)
knowledge_chunks(id, knowledge_entry_id, class_id, chunk_index,
                 start_offset, end_offset, chunk_text, index_status,
                 embedding_model, created_at)
```

- `materials`、`knowledge_entries`、`knowledge_chunks` 均带**非空且带索引**的 `class_id`。
- `knowledge_chunks` 上建 `FULLTEXT(chunk_text) WITH PARSER ngram`。
- `index_status` 取值 `pending | ready | failed`：`ready` 才参与检索；`failed` 保留记录但不写向量。
- 保留 `knowledge_entries.body_text` 作为原文来源；检索只读 `chunk_text`，不存在第二套检索体系。

### 4. 切片

`Chunker` 接口，输入 `body_text` 与本次策略，输出若干切片（文本、序号、在**待切分文本**中的字符区间）。三种实现：

| 策略 | 规则 |
| --- | --- |
| `auto`（默认） | 最大 800 字、重叠 80 字；优先在空行、换行、句号处断开 |
| `custom` | 最大长度 100–2000 字、重叠 0%–50%；无断点按最大长度截断；可选折叠空白、去链接 |
| `hierarchy` | 按 `#`、`##`、`###` 分章，标题保留在章内；过长章节再按 auto 窗口切 |

- **字符单位为 rune**；`start_offset`/`end_offset` 为相对**预处理后文本**的 absolute rune 偏移——预处理改写输入后，偏移不再对应原文件下标，这一点写入文档与命中展示。
- 预处理（折叠空白、去链接）只作用于切分与嵌入输入，`body_text` 保持上传原文。
- 兜底：`overlap >= max` 拒绝；空正文/全空白返回空切片。

### 5. 中文全文检索

`FULLTEXT ... WITH PARSER ngram`，`ngram_token_size = 2`，在 `docker-compose.yml` 显式设置，与 proposal/design 声明一致。已知限制：token 长度 2 时**单字查询无法匹配**，属 ngram 固有行为，写入文档。keyword 路径判定「有效命中」为 `MATCH ... AGAINST(q IN NATURAL LANGUAGE MODE)` 分数 > 0（可选配置最小分，默认 0）。

### 6. 嵌入

- 仅服务端调用兼容 OpenAI `/embeddings` 的**外部嵌入网关**；地址、API Key、模型名、维度经环境变量注入，密钥不下发浏览器。
- 写入 Qdrant 前校验**实际向量维度 == collection 维度**，不一致即失败。
- 切片逐个/分批嵌入；设置超时与有限重试；嵌入失败将切片标记 `failed`，不写不完整向量。

### 7. Qdrant 契约

- Collection `campusclaw_chunks`，度量**余弦**，维度来自配置。
- 点主键 = `knowledge_chunks.id`。
- payload 仅含 `class_id`、`material_id`、`knowledge_entry_id`、`chunk_id`、`chunk_index`；**不含正文**。
- 向量查询 MUST 带 `class_id` 过滤。

### 8. 检索链路

```
POST /api/search {q, mode, limit, offset}
   |
   +-- keyword: MySQL FULLTEXT(ngram), WHERE class_id=sid AND index_status='ready'
   |            score>0 视为有依据
   |
   +-- vector : embed(q) -> Qdrant filter{class_id=sid}, 丢弃 cosine < 0.35
   |            回 MySQL 取正文，再次核对 class_id
   |
   +-- hybrid : 两路各自过滤命中 -> RRF(k=60) 融合名次 -> top-K
               缺席的一路不贡献分数
   |
   命中 -> {chunk_id, material_title, chunk_index, start_offset, end_offset, snippet}
   无命中 -> status="no_evidence", message="资料中未找到相关内容"
```

- 空查询 400。
- Qdrant 不可用：keyword 仍可返回；vector/hybrid 返回 503，不编造分数。

### 9. 相关性判据：门槛加在单路绝对分上，不加在 RRF 分上

RRF 分只由名次决定，与真实相关度无关，**不能**用其当「有依据」阈值。做法：**先按 keyword 有效命中 / vector 余弦 ≥ 0.35 在各自路径内过滤**，再对幸存候选做 RRF 排序。阈值与 `k` 均为配置项（默认 `0.35`、`60`）。这直接满足「不凑低相关内容」。

### 10. `POST /api/ask`

```
{q, history} -> hybrid 取前 4 条有依据切片
   有 -> system(服务端写) + [材料标题, 切片序号, 切片正文] 编号 [1..n] + 最新提问
         -> 对话网关 -> {answer, citations}（[n] 与 citations 同序）
   无 -> 不调用对话网关 -> {answer:"资料中未找到相关内容", status:"no_evidence", citations:[]}
```

- 对话模块不接触向量分量、其他班级切片、客户端 system 消息；本课不做流式。
- 对模型返回的越界引用（如 `[5]`）做校验/剥离。

### 11. 两库一致性

- **MySQL `knowledge_chunks` 为事实来源**；Qdrant 可重建。
- 顺序：事务提交 `materials` + `knowledge_entries` → 切分写 `knowledge_chunks`（`pending`）→ 嵌入 → 按主键**幂等 upsert** Qdrant → 置 `ready`；失败置 `failed`。
- 幂等 upsert 保证重试不产生错误向量；不引入 MQ 或分布式事务。
- **重建路径**：`POST /api/materials/{id}/reindex` + 可重复执行的补嵌命令；重建时**先删旧切片与旧向量**，再按本次策略写入。补嵌只处理 `pending`/`failed` 或指定材料。
- 嵌入模型/维度变更 → 触发全量重建。

### 12. 数据迁移（SQLite → MySQL）

一次性、可重复的迁移子命令（Go，纯 Go SQLite 驱动读取旧库）：

1. 确保 MySQL schema 就绪。
2. 复制 `classes`、`users`（bcrypt `$2b$` 哈希可直接被 Go `golang.org/x/crypto/bcrypt` 校验）、`materials`；`file_path` 由旧 `/app/uploads/...` 映射到新挂载路径。
3. 复制 `knowledge_entries.body_text`（保留原文来源）。
4. 对每份已有正文触发**重新切片 + 生成 embedding + 写 Qdrant**；种子材料无正文则不产切片。
5. 不引入迁移框架；不保留旧 SQLite 作为运行时存储。

### 13. 配置（环境变量，`.env.example` 列出）

| 变量 | 用途 |
| --- | --- |
| `TOKEN_SECRET` | 令牌哈希（HMAC-SHA256）密钥，必需 |
| `DB_DSN` / `MYSQL_*` | MySQL 连接口令等 |
| `UPLOAD_DIR` | 上传目录（仅挂载到 `api`） |
| `QDRANT_URL`、`QDRANT_COLLECTION` | 向量库 |
| `EMBEDDING_BASE_URL`、`EMBEDDING_API_KEY`、`EMBEDDING_MODEL`、`EMBEDDING_DIM` | 嵌入网关与维度 |
| `CHAT_BASE_URL`、`CHAT_API_KEY`、`CHAT_MODEL` | 对话网关 |
| `VECTOR_MIN_SCORE`（0.35）、`RRF_K`（60） | 检索参数 |
| `SESSION_TTL`、`MAX_UPLOAD_BYTES` | 令牌有效期与上传上限 |

### 14. API 契约（与线上实例一致）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/login` | `{username,password}` → `{token, user}`；token 仅此响应返回，不下发 Cookie |
| POST | `/api/logout` | 删除会话行（吊销当前令牌） |
| GET | `/api/me` | 当前用户（用于 SPA 启动校验令牌） |
| GET | `/api/materials?page&page_size&title` | 本班分页列表 |
| POST | `/api/materials` | multipart：`file,title,chunk_*`；返回 `title,chunk_strategy,chunk_count` |
| GET | `/api/materials/{id}` | 材料详情（含向量/切片调试信息） |
| GET | `/api/materials/{id}/vectors` | 切片与向量对照 |
| POST | `/api/materials/{id}/reindex` | `{chunk_strategy,chunk_separator,chunk_max_chars,chunk_overlap_percent,chunk_collapse_ws,chunk_strip_links}` |
| POST | `/api/search` | `{q,mode,limit,offset}`；未认证 401 |
| POST | `/api/ask` | `{q,history}` |
| GET | `/health` | 无需认证，不返回材料内容 |

除 `POST /api/login` 与 `GET /health` 外，所有 `/api/*` MUST 要求 `Authorization: Bearer <token>`；令牌缺失、无效或过期统一返回 401。前端由 Nginx 托管，非 `/api/*` 的前端路由 fallback 到 `index.html`。

### 15. 前端框架与构建

- 采用 **Vue 3 + Vite + TypeScript**，`vue-router` 管理前端路由；产物输出到 `web/dist`。
- Dockerfile 多阶段构建：Node 阶段 `npm ci && npm run build`，产物拷入 Nginx 镜像；`docker compose up --build` 一步完成前端构建与托管。
- SPA 只通过 `/api/*` 访问后端（同源，无需 CORS）；开发期用 Vite proxy 指向本地 Go 服务。
- 备选：React 18 + Vite（能力等价）；服务端模板（与 token/SPA 要求不符）。

### 16. 北大红主题

- 设计令牌（CSS 变量）集中定义：主色 `--pku-red: #94070A`、辅助色 `--pku-red-alt: #BE2126`，并派生 hover / 边框 / 浅背景等变量。
- 主题覆盖登录、材料列表 / 上传 / 详情、检索与问答的主要元素（主按钮、链接、标题强调、聚焦态）。
- 正文与背景保持高对比（WCAG AA，普通文本 ≥ 4.5:1）；主色用于强调而非大面积正文背景。
- 备选：逐组件硬编码颜色（不可维护，弃用）。

## Risks / Trade-offs

- [ngram token=2 使单字查询无匹配] → 文档写明，并用长度 ≥ 2 的查询与样本验收。
- [向量阈值 0.35 依赖嵌入模型标定，换模型会漂移] → 阈值配置化；换模型走全量重建。
- [两库非事务，可能出现 chunks 有、向量缺] → MySQL 为事实来源、`index_status` 标记、幂等 upsert、可重复重建。
- [会话表增长] → `expires_at` + 定期清理；登出删除行。
- [预处理后偏移不等于原文件下标] → 命中展示标注偏移相对预处理文本；`body_text` 始终为原文。
- [客户端直连 Qdrant / 网关会绕过认证] → Qdrant 与网关不映射宿主端口、不作为浏览器入口。
- [`localStorage` 中的令牌可被 XSS 窃取] → 严格 CSP、输出转义、令牌短 TTL、登出即吊销；不用 `innerHTML` 渲染不可信内容。
- [SPA 构建增加镜像步骤] → 多阶段 Docker 构建，Node 构建产物拷入 Nginx 镜像，`docker compose up --build` 一步完成。
- [前端路由 fallback 误吞 API 404] → Nginx 仅对非 `/api/*`、非静态资源的路径回退 `index.html`。
- [迁移把旧 `/app/uploads` 路径写死] → 迁移脚本做路径映射并校验文件存在。

## Migration Plan

1. 建 MySQL schema（五张表 + `knowledge_chunks` + 全文索引）。
2. 运行 SQLite→MySQL 迁移子命令，导入班级/用户/材料/正文并映射路径。
3. 对已有正文执行重建（切片 + 嵌入 + 写 Qdrant）。
4. 在 Compose 内启动 Nginx（托管 SPA）+ Go + MySQL + Qdrant，跑 `/health` 并确认前端深层路由可刷新。
5. 重跑第 3 课场景（登录、学生 403、跨班 404、上传可见）+ 第 4 课新场景（三模式、隔离、可追溯、无依据不调用模型）。
6. 回滚：保留旧 SQLite 与上传目录副本，必要时切回旧镜像；两库均可重建，不丢原文。

## Open Questions

- 嵌入/对话网关的实际地址、模型名与向量维度取值：属部署前必须提供的配置，不影响规约、方案与任务拆分，配置就绪前用占位并在 `.env.example` 标注。
- keyword 路径是否额外设「最小全文分」：默认 `0`（仅需 score>0），如需更严可后续加配置，不改方案。
