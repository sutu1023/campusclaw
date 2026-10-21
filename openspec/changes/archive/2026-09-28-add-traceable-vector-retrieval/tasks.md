# Tasks

## 1. 背包与依赖（Go / Nginx / MySQL / Qdrant / Vue）

- [x] 1.1 初始化 Go module 与目录骨架（`cmd/api`、`internal/auth`、`internal/materials`、`internal/chunk`、`internal/embed`、`internal/retrieval`、`internal/store`）— verify: `go build ./...` 成功
- [x] 1.2 添加 Go 依赖（HTTP 路由、MySQL 驱动、`golang.org/x/crypto/bcrypt`、纯 Go SQLite 驱动、Qdrant 客户端）— verify: `go mod tidy` 后 `go build ./...` 成功
- [x] 1.3 初始化前端工程（Vue 3 + Vite + TypeScript），启用 TS 严格模式与 ESLint，构建产物输出到 `web/dist` — verify: `npm ci && npm run build` 生成 `web/dist/index.html`
- [x] 1.4 编写 `.env.example`，列出 design §13 的全部环境变量（含 `TOKEN_SECRET`、`SESSION_TTL` 及网关/检索配置的占位与说明），确保无真实密钥 — verify: `git grep -n "API_KEY=" .env.example` 仅见占位符

## 2. 数据模型与令牌存储

- [x] 2.1 实现 MySQL schema 与迁移：`classes / users / sessions / materials / knowledge_entries / knowledge_chunks`，租户列 `class_id` 非空且带索引，`knowledge_chunks` 建 `FULLTEXT(chunk_text) WITH PARSER ngram` — verify: `SHOW CREATE TABLE knowledge_chunks` 含全文索引；`SHOW VARIABLES LIKE 'ngram_token_size'` 为 2
- [x] 2.2 实现种子脚本：班级 A/B、教师 A、学生 A1/B1、两班可区分材料，口令 bcrypt 哈希 — verify: `SELECT username,role,class_id FROM users` 三条齐全；哈希以 `$2` 开头
- [x] 2.3 实现 `store` 层的按 `class_id` 查询 helper（列表/详情/正文），任何业务查询禁止传入客户端 `class_id` — verify: 单元测试对 class A 查询返回条数 ≥ 1 且不含 B 班标题
- [x] 2.4 实现令牌存储：`sessions(token_hash, user_id, expires_at, created_at)`，以 `HMAC-SHA256(TOKEN_SECRET, token)` 存哈希，支持按哈希查找、过期判定与删除 — verify: 单元测试覆盖「过期令牌查不到」「明文令牌不入库」

## 3. 认证与授权（token 方式）

- [x] 3.1 实现 `POST /api/login`：校验 bcrypt、**删除该用户旧会话并签发新令牌**、令牌仅经响应体返回（不下发 Cookie）— verify: 集成测试断言响应含新令牌、`sessions` 无明文、`GET /api/me` 能读到角色与班级
- [x] 3.2 实现认证中间件：解析 `Authorization: Bearer`、计算哈希查 `sessions`、读 `users` 得 `role`/`class_id` 写入上下文，缺失/无效/过期统一 401 — verify: 无令牌请求 `/api/materials` 返回 401 且 body 无材料字段
- [x] 3.3 实现 `POST /api/logout`（删除会话行）与 `GET /api/me` — verify: 登出后携原令牌请求 `/api/me` 返回 401
- [x] 3.4 实现教师角色校验：学生请求上传在**落盘前**返回 403 — verify: 学生 A1 上传返回 403，且 `materials` 行数与上传目录文件数不变
- [x] 3.5 登录失败原因对外统一（账号不存在/口令错误同码同文案）；缺失必需环境变量（含 `TOKEN_SECRET`）时启动失败 — verify: 两种失败响应体与状态码一致；不设 `TOKEN_SECRET` 启动报错

## 4. 材料模块与上传

- [x] 4.1 实现 `GET /api/materials`（分页、`title` 过滤、仅本班）与 `GET /api/materials/{id}`（跨班与不存在均 404）— verify: 学生 A1 列表仅含 A 班；教师 A 访问 B 班 id 返回 404 且无 B 班内容
- [x] 4.2 实现 `POST /api/materials`：白名单 `.txt/.md` + 大小上限 → 服务端生成存储名落盘 → 同事务写 `materials` 与 `knowledge_entries.body_text`（原文）—— verify: 上传后两表各增 1 行、`body_text` 等于原文；非法扩展名/超限返回 4xx 且无文件无记录
- [x] 4.3 上传失败清理：任一步失败回滚两表并删除已写文件 — verify: 人为使事务失败后无孤儿记录与孤儿文件
- [x] 4.4 在 README/接口文档补充材料接口与上传参数（`chunk_*`）说明 — verify: 按文档用 curl 可完成一次上传

## 5. 切片模块

- [x] 5.1 定义 `Chunker` 接口与切片结构（文本、`chunk_index`、`start_offset`、`end_offset`，rune 偏移）— verify: 单元测试对中文文本校验偏移为 rune 且 `text == runes[start:end]`
- [x] 5.2 实现 `auto`：最大 800 字、重叠 80 字，优先空行/换行/句号断开；处理 `overlap >= max` 与空正文 — verify: 单元测试覆盖边界与不丢字
- [x] 5.3 实现 `custom`（100–2000 字、0–50%、强制截断、可选折叠空白/去链接）与 `hierarchy`（按 `#/##/###` 分章、过长再窗口）— verify: 单元测试对同一带标题文本分别产出预期切片
- [x] 5.4 把切片写入 `knowledge_chunks`，`index_status='pending'`；预处理只作用于切分输入，`body_text` 不变 — verify: 上传后切片行数 ≥ 1 且 body_text 未变

## 6. 嵌入与 Qdrant

- [x] 6.1 实现嵌入客户端（服务端调用外部网关，超时+有限重试），维度来自配置 — verify: 网关桩返回固定维度，客户端解析成功
- [x] 6.2 初始化 Qdrant collection `campusclaw_chunks`（余弦、配置维度，已存在则校验维度一致）— verify: collection 信息维度与配置一致；不一致时启动失败
- [x] 6.3 实现按主键幂等 upsert，payload 仅含 `class_id/material_id/knowledge_entry_id/chunk_id/chunk_index`，不含正文 — verify: 取点 payload 字段集合与 design 一致，且无正文字段
- [x] 6.4 嵌入/写入成功置 `index_status='ready'`，失败置 `failed` 且不写不完整向量 — verify: 注入失败后切片为 failed、Qdrant 无对应点、`body_text` 仍在

## 7. 检索

- [x] 7.1 实现 keyword 路径：MySQL ngram 全文、`class_id=会话班级`、仅 `index_status='ready'`、score>0 视为有效命中 — verify: 原词检索有命中，且不产生问句向量、不访问 Qdrant（桩断言）
- [x] 7.2 实现 vector 路径：问句嵌入 → Qdrant 按会话班级过滤 → 丢弃 cosine<`VECTOR_MIN_SCORE` → 回表取正文并**再次核对班级** — verify: 低分候选被丢弃；回表后无他班切片
- [x] 7.3 实现 hybrid：两路各自过滤后按 RRF（`RRF_K`）融合，缺席一路不贡献分数 — verify: 单元测试校验融合名次符合 RRF 公式，且非分数相加
- [x] 7.4 实现 `POST /api/search` 返回 `{query_vector?, hits, status, message}`，命中含 `chunk_id/material_title/chunk_index/start_offset/end_offset/snippet`，空查询 400 — verify: 三条检索模式端到端返回结构符合契约
- [x] 7.5 实现 Qdrant 不可用降级：keyword 仍返回，vector/hybrid 返回 503 且无编造分数 — verify: 停 Qdrant 后 keyword 有结果、vector/hybrid 为 503
- [x] 7.6 班级隔离：请求体/参数/头中的 `class_id` 一律丢弃；跨班检索返回 200 空命中 — verify: A 会话带 B 的 class_id 检索 B 专属词，hits 为空
- [x] 7.7 无依据处理：过滤后无候选返回空命中 + 「资料中未找到相关内容」，不以低相关凑数 — verify: 无关提问 hits 为空且 message 匹配

## 8. 问答

- [x] 8.1 实现 `POST /api/ask`：本班 hybrid 取前 4 条有依据切片，服务端写 system，组装标题/序号/正文带编号 — verify: 桩对话网关记录的输入含编号与切片正文，且无向量分量
- [x] 8.2 有依据时返回 `{answer, citations}`，`[n]` 与 citations 同序；越界引用做校验 — verify: 桩返回含 `[1][2]`，citations 顺序一致
- [x] 8.3 无命中时不调用对话网关，返回固定文案、`citations` 为空；丢弃客户端 system 消息 — verify: 无关提问时桩网关零调用；携带 client system 被忽略
- [x] 8.4 更新 README/接口文档补充 `/api/search`、`/api/ask` 字段与示例 — verify: 文档示例可直接复现

## 9. 重建与一致性

- [x] 9.1 实现 `GET /api/materials/{id}/vectors`（切片与向量对照，含 `index_status`、维度、payload 标识）— verify: 返回字段与 design §14 一致且仅本班
- [x] 9.2 实现 `POST /api/materials/{id}/reindex`：先删旧切片与旧向量，再按本次策略重切、重嵌、重写 — verify: 重建后旧 point 不残留，切片数随策略变化
- [x] 9.3 实现可重复执行的补嵌命令，只处理 `pending`/`failed` 或指定材料；重复执行幂等 — verify: 连续执行两次结果一致、无重复 point

## 10. 数据迁移（SQLite → MySQL）

- [x] 10.1 实现一次性迁移子命令：读旧 SQLite 的 `classes/users/materials/knowledge_entries`，映射 `/app/uploads` 路径并校验文件存在 — verify: 迁移后 MySQL 行数与旧库一致，路径可读
- [x] 10.2 迁移后对已有正文触发重建（切片+嵌入+写 Qdrant）— verify: 迁移材料在 keyword 与 vector 检索中均可命中
- [x] 10.3 写迁移说明（来源、映射规则、回滚：保留旧库副本）— verify: 按文档从旧库副本可在干净 MySQL 上重跑

## 11. Compose 与健康检查

- [x] 11.1 编写 `docker-compose.yml`：`web`(Nginx) + `api`(Go) + `db`(MySQL) + `qdrant`；MySQL 不映射宿主端口；Qdrant/网关不作为浏览器入口；`--ngram-token-size=2` — verify: `docker compose up --build -d` 后四容器 running，宿主不可直连 MySQL
- [x] 11.2 配置 Nginx：托管 SPA 构建产物、对非 `/api/*` 的前端路由 fallback 到 `index.html`、`/api` 反代到 Go；上传目录不被暴露为静态目录 — verify: 浏览器仅经 Nginx 访问 `/api`，直请上传路径不可得，深层路由刷新不 404
- [x] 11.3 实现 `GET /health`（无需认证、不返回材料内容）并配置 healthcheck — verify: 未登录 `curl -sf /health` 返回 200 `{"status":"ok"}`
- [x] 11.4 数据卷持久化：MySQL 数据与上传目录 mount；`down` 再 `up` 不丢数据 — verify: 上传一条后 down/up，列表与切片仍在
- [x] 11.5 多阶段 Docker 构建：Node 阶段 `npm ci && npm run build`，产物拷入 Nginx 镜像 — verify: `docker compose up --build` 后根路径返回 SPA 入口
- [x] 11.6 更新 README：Compose 启动、必需环境变量、登录 URL、`/health`、四服务拓扑与前端开发/构建方式 — verify: 按 README 从零可打开登录页并 curl health

## 12. 前端 SPA（Vue 3 + Vite + TypeScript）

- [x] 12.1 搭建 SPA 外壳与路由（登录、材料列表/上传/详情、检索、问答），未登录路由重定向登录页 — verify: 路由可刷新；未登录访问受保护路由跳登录
- [x] 12.2 实现 API 客户端：注入 `Authorization: Bearer`、统一处理 401（清令牌并跳登录）、令牌不出现在 URL — verify: 单元测试覆盖 401 处理与请求头注入
- [x] 12.3 登录页接入 `POST /api/login`，令牌存 `localStorage`，应用启动以 `/api/me` 校验 — verify: 登录后刷新仍保持登录；伪造/过期令牌被清并跳登录
- [x] 12.4 材料页面：列表、教师上传、详情（含切片/向量调试信息），按角色显示上传入口 — verify: 教师可见上传入口、学生不可见；学生写请求仍被服务端 403
- [x] 12.5 检索与问答页面：模式切换（keyword/vector/hybrid）、展示命中可追溯字段（标题/序号/区间/摘录）与 `[n]` 引用 — verify: 对样例查询展示可点击回源材料
- [x] 12.6 应用北大红主题：CSS 变量主色 `#94070A`、辅助色 `#BE2126`，覆盖登录/材料/检索/问答主要元素，正文对比度满足 WCAG AA — verify: 对比度检查工具对正文 ≥ 4.5:1
- [x] 12.7 前端文档：开发/构建命令、API base 配置、主题色板 — verify: 按文档 `npm run dev` 可本地联调 `/api`

## 13. 集成验收（第 3 课基线、第 4 课新场景、token 与前端）

- [x] 13.1 重跑第 3 课场景：教师/学生登录、学生上传 403、跨班按 ID 404、上传后本班列表可见 — verify: 记录四条实测结果
- [x] 13.2 第 4 课检索场景：keyword/vector/hybrid 三模式、班级隔离（注入 class_id 无效、跨班空命中）、可追溯字段、无依据不编造 — verify: 逐条执行 `knowledge-retrieval` 场景并记录结果
- [x] 13.3 问答场景：有依据带 `[1][2]`、无依据不调用模型且 citations 为空 — verify: 记录实测（含桩/真实网关调用计数）
- [x] 13.4 token 与前端场景：SPA 登录 → 持 Bearer 访问受保护接口 → 登出后同令牌 401 → 401 自动跳登录；主要界面主色为 `#94070A` — verify: 记录请求头、状态码与页面主色
- [x] 13.5 `openspec validate add-traceable-vector-retrieval --strict` 通过 — verify: exit 0 且无 error
