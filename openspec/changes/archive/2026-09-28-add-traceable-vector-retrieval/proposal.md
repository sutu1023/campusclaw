# Proposal

## Why

CampusClaw 第 3 课课件要求 **Go 服务 + Nginx + MySQL + 服务端会话**，而本仓库现有 `auth-upload` 以 Flask + SQLite + 客户端签名 Cookie 会话 + Jinja 模板实现，与课件基线不一致。第 4 课要在「同一套系统」上增加**可追溯知识库检索**：把材料正文切分为切片、向量写入 Qdrant，支持关键字 / 向量 / 混合三种检索，并在有依据时生成带出处标注的简短回答；没有依据时明确返回未找到，不得编造。同时，项目要求前后端通信统一为 **token 方式**、前端界面采用**北大红**视觉风格。因此本变更在补齐第 3 课基线、实现第 4 课检索与问答之外，一并完成**鉴权 token 化**与**前端 SPA 化 + 北大红主题**。

## What Changes

- **BREAKING：拉齐第 3 课基线。** Web 层由 Flask 改为 **Go**；数据库由 SQLite 改为 **MySQL**；会话状态保留在服务端 `sessions` 表，但传输方式由客户端签名 Cookie 改为 **token 方式**（登录签发高熵不透明令牌，客户端以 `Authorization: Bearer` 携带，登录成功换发新令牌、登出即时吊销）；增加 **Nginx** 反代 `/api` 并托管前端；接口统一到 `/api/*`；核心表收敛为 `classes / users / sessions / materials / knowledge_entries` 五张（材料与知识库表均带 `class_id` 租户列）。
- **BREAKING：前端重构为独立 SPA。** 由服务端 Jinja 模板改为 **Vue 3 + Vite + TypeScript 单页应用**，构建产物由 Nginx 托管并提供前端路由 history fallback；业务页面不再由服务端渲染。
- **UI 主题：北大红。** 统一视觉主题，主色 `#94070A`、辅助色 `#BE2126`，覆盖登录、材料、检索与问答界面。
- **新增切片与入库：** 上传成功后按策略把 `knowledge_entries.body_text` 切分为 `knowledge_chunks`（正文、序号、字符区间、状态）；支持 `auto` / `custom` / `hierarchy` 三种策略与预处理，默认 `auto`（约 800 字、重叠 80 字）。
- **新增向量化：** 服务端调用外部嵌入网关，把每个切片转为固定维度向量，按切片主键**幂等**写入 Qdrant（collection `campusclaw_chunks`，余弦度量，payload 仅含标识、不含正文）。
- **新增检索：** `POST /api/search` 支持 `keyword`（MySQL FULLTEXT ngram，token=2）、`vector`（Qdrant，余弦阈值 0.35）、`hybrid`（两路各自过滤后按 RRF `k=60` 融合，默认）；命中至少含材料标题、切片序号、字符区间、摘录，并支持打开对应材料。
- **新增问答：** `POST /api/ask` 先做本班混合检索，取前 4 条有依据切片，带编号与正文调用对话网关，回答以 `[1]`、`[2]` 标注出处；无命中时**不调用模型**，返回固定文案、`citations` 为空。
- **新增重建路径：** 提供可重复执行的补嵌 / 重建（`POST /api/materials/{id}/reindex` 等），切换策略时先删旧切片与旧向量再重建。
- **保持隔离与安全：** 班级仅取自令牌对应的服务端会话，关键字与向量两条路径均按会话班级过滤，回表取正文时再次核对；请求体中携带的 `class_id` 一律丢弃；跨班检索对外表现为空命中；跨班按 ID 访问材料仍为 404；登录失败原因对外统一；密钥（含 `TOKEN_SECRET`）与网关配置仅经环境变量注入。
- **文档与编排：** 更新 `README`、`docker-compose.yml`、`.env.example`，加入 MySQL、Qdrant、Nginx、前端构建与嵌入/对话网关配置；`/health` 不要求认证、不返回材料内容。

## Capabilities

### New Capabilities

- `knowledge-retrieval`: 正文切片策略、切片与向量的入库、关键字 / 向量 / 混合三种检索、命中可追溯、无依据不编造、以及 `POST /api/ask` 的取据与引用标注行为。
- `web-frontend`: 独立 SPA 的托管与前端路由、token 方式的 API 通信与 401 处理、北大红视觉主题、按角色渲染的页面范围。

### Modified Capabilities

- `auth-upload`: 鉴权由客户端签名 Cookie 会话改为服务端可撤销的 **token 方式**（`Authorization: Bearer` 携带的不透明令牌，`sessions` 存哈希）；Docker Compose 编排纳入前端 SPA 构建与 Nginx 托管。

## Impact

- **服务**：新增 Go 服务与 Nginx 容器；替换现有 Flask 应用与 `requirements.txt`/`run.py` 入口。
- **前端**：新增 Vue 3 + Vite + TypeScript 工程；移除 Jinja 模板；Nginx 托管构建产物并对前端路由 fallback。
- **数据**：新增 `sessions`、`knowledge_chunks` 表；`sessions` 存令牌哈希而非明文；`knowledge_entries.body_text` 保留为原文来源；`materials` 等业务数据需从现有 SQLite 迁移至 MySQL。
- **鉴权**：由 Cookie 会话改为 `Authorization: Bearer` token；未认证 API 统一 401；不再需要 CSRF token，但需防御 XSS 对令牌的窃取。
- **依赖服务**：新增 MySQL、Qdrant、外部嵌入网关与外部对话网关（地址、密钥、模型、维度经环境变量配置）。
- **接口**：新增 `POST /api/search`、`POST /api/ask`、`GET /api/materials/{id}/vectors`、`POST /api/materials/{id}/reindex`；`POST /api/login` 返回令牌；既有登录 / 材料接口迁移到 `/api/*`。
- **验收**：第 3 课既有场景（登录、学生上传 403、跨班 404、上传后本班可见）需在新栈上重新执行；第 4 课新增检索、隔离、可追溯与问答场景；新增 token 鉴权、SPA 与北大红主题场景。

## Non-goals（非目标）

- **不做流式长对话 / 会话编排**：流式输出、多轮会话管理属第 5 课。
- **不引入检索编排框架**：不使用 LangChain、LlamaIndex、Dify 等；由 Go 服务自行完成切分、嵌入与检索。
- **不做交叉编码器重排序**：本课不实现 rerank。
- **不引入消息队列 / 分布式事务**：两库一致性以「MySQL 为事实来源 + 幂等 upsert + 重建路径」保证，不引入 MQ。
- **不自部署嵌入 / 对话模型**：仅调用外部网关。
- **不做多租户 / 多校 / SSO**：班级隔离即本课边界。
- **不做 JWT / 刷新令牌 / 第三方登录**：令牌为服务端可撤销的不透明随机值。
- **不做服务端渲染 / 同构渲染**：前端为纯客户端 SPA。
- **不做 PDF / Word / 图片解析**：仍限 `.txt` / `.md`。
