# Tasks

## 1. 背包与依赖（Go / Nginx / MySQL / Qdrant）

- [ ] 1.1 初始化 Go module 与目录骨架（`cmd/api`、`internal/auth`、`internal/materials`、`internal/chunk`、`internal/embed`、`internal/retrieval`、`internal/store`）— verify: `go build ./...` 成功
- [ ] 1.2 添加依赖（HTTP 路由、MySQL 驱动、`golang.org/x/crypto/bcrypt`、纯 Go SQLite 驱动、Qdrant 客户端）— verify: `go mod tidy` 后 `go build ./...` 成功
- [ ] 1.3 编写 `.env.example`，列出 design §13 的全部环境变量（含占位与说明），确保无真实密钥 — verify: `git grep -n "API_KEY=" .env.example` 仅见占位符

## 2. 数据模型与会话存储

- [ ] 2.1 实现 MySQL schema 与迁移：`classes / users / sessions / materials / knowledge_entries / knowledge_chunks`，租户列 `class_id` 非空且带索引，`knowledge_chunks` 建 `FULLTEXT(chunk_text) WITH PARSER ngram` — verify: `SHOW CREATE TABLE knowledge_chunks` 含全文索引；`SHOW VARIABLES LIKE 'ngram_token_size'` 为 2
- [ ] 2.2 实现种子脚本：班级 A/B、教师 A、学生 A1/B1、两班可区分材料，口令 bcrypt 哈希 — verify: `SELECT username,role,class_id FROM users` 三条齐全；哈希以 `$2` 开头
- [ ] 2.3 实现 `store` 层的按 `class_id` 查询 helper（列表/详情/正文），任何业务查询禁止传入客户端 `class_id` — verify: 单元测试对 class A 查询返回条数 ≥ 1 且不含 B 班标题
- [ ] 2.4 实现服务端会话存储：`sessions` 表读写、过期清理、删除会话行 — verify: 单元测试覆盖「过期会话查不到」

## 3. 认证与授权

- [ ] 3.1 实现 `POST /api/login`：校验 bcrypt、**删除旧会话并签发新会话标识**、设置 HttpOnly+SameSite Cookie — verify: 集成测试断言响应不新建/不沿用旧标识；`/api/me` 能读到角色与班级
- [ ] 3.2 实现认证中间件：每次请求查会话→读 `users` 得 `role`/`class_id` 写入上下文，未登录 API 返回 401 统一文案 — verify: 无 Cookie 请求 `/api/materials` 返回 401 且 body 无材料字段
- [ ] 3.3 实现 `POST /api/logout`（删除会话行）与 `GET /api/me` — verify: 登出后携原 Cookie 请求 `/api/me` 返回 401
- [ ] 3.4 实现教师角色校验：学生请求上传在**落盘前**返回 403 — verify: 学生 A1 上传返回 403，且 `materials` 行数与上传目录文件数不变
- [ ] 3.5 登录失败原因对外统一（账号不存在/口令错误同码同文案）；缺失必需环境变量时启动失败 — verify: 两种失败响应体与状态码一致；不设 `SESSION_SECRET` 启动报错

## 4. 材料模块与上传

- [ ] 4.1 实现 `GET /api/materials`（分页、`title` 过滤、仅本班）与 `GET /api/materials/{id}`（跨班与不存在均 404）— verify: 学生 A1 列表仅含 A 班；教师 A 访问 B 班 id 返回 404 且无 B 班内容
- [ ] 4.2 实现 `POST /api/materials`：白名单 `.txt/.md` + 大小上限 → 服务端生成存储名落盘 → 同事务写 `materials` 与 `knowledge_entries.body_text`（原文）—— verify: 上传后两表各增 1 行、`body_text` 等于原文；非法扩展名/超限返回 4xx 且无文件无记录
- [ ] 4.3 上传失败清理：任一步失败回滚两表并删除已写文件 — verify: 人为使事务失败后无孤儿记录与孤儿文件
- [ ] 4.4 在 README/接口文档补充材料接口与上传参数（`chunk_*`）说明 — verify: 按文档用 curl 可完成一次上传

## 5. 切片模块

- [ ] 5.1 定义 `Chunker` 接口与切片结构（文本、`chunk_index`、`start_offset`、`end_offset`，rune 偏移）— verify: 单元测试对中文文本校验偏移为 rune 且 `text == runes[start:end]`
- [ ] 5.2 实现 `auto`：最大 800 字、重叠 80 字，优先空行/换行/句号断开；处理 `overlap >= max` 与空正文 — verify: 单元测试覆盖边界与不丢字
- [ ] 5.3 实现 `custom`（100–2000 字、0–50%、强制截断、可选折叠空白/去链接）与 `hierarchy`（按 `#/##/###` 分章、过长再窗口）— verify: 单元测试对同一带标题文本分别产出预期切片
- [ ] 5.4 把切片写入 `knowledge_chunks`，`index_status='pending'`；预处理只作用于切分输入，`body_text` 不变 — verify: 上传后切片行数 ≥ 1 且 body_text 未变

## 6. 嵌入与 Qdrant

- [ ] 6.1 实现嵌入客户端（服务端调用外部网关，超时+有限重试），维度来自配置 — verify: 网关桩返回固定维度，客户端解析成功
- [ ] 6.2 初始化 Qdrant collection `campusclaw_chunks`（余弦、配置维度，已存在则校验维度一致）— verify: collection 信息维度与配置一致；不一致时启动失败
- [ ] 6.3 实现按主键幂等 upsert，payload 仅含 `class_id/material_id/knowledge_entry_id/chunk_id/chunk_index`，不含正文 — verify: 取点 payload 字段集合与 design 一致，且无正文字段
- [ ] 6.4 嵌入/写入成功置 `index_status='ready'`，失败置 `failed` 且不写不完整向量 — verify: 注入失败后切片为 failed、Qdrant 无对应点、`body_text` 仍在

## 7. 检索

- [ ] 7.1 实现 keyword 路径：MySQL ngram 全文、`class_id=会话班级`、仅 `index_status='ready'`、score>0 视为有效命中 — verify: 原词检索有命中，且不产生问句向量、不访问 Qdrant（桩断言）
- [ ] 7.2 实现 vector 路径：问句嵌入 → Qdrant 按会话班级过滤 → 丢弃 cosine<`VECTOR_MIN_SCORE` → 回表取正文并**再次核对班级** — verify: 低分候选被丢弃；回表后无他班切片
- [ ] 7.3 实现 hybrid：两路各自过滤后按 RRF（`RRF_K`）融合，缺席一路不贡献分数 — verify: 单元测试校验融合名次符合 RRF 公式，且非分数相加
- [ ] 7.4 实现 `POST /api/search` 返回 `{query_vector?, hits, status, message}`，命中含 `chunk_id/material_title/chunk_index/start_offset/end_offset/snippet`，空查询 400 — verify: 三条检索模式端到端返回结构符合契约
- [ ] 7.5 实现 Qdrant 不可用降级：keyword 仍返回，vector/hybrid 返回 503 且无编造分数 — verify: 停 Qdrant 后 keyword 有结果、vector/hybrid 为 503
- [ ] 7.6 班级隔离：请求体/参数/头中的 `class_id` 一律丢弃；跨班检索返回 200 空命中 — verify: A 会话带 B 的 class_id 检索 B 专属词，hits 为空
- [ ] 7.7 无依据处理：过滤后无候选返回空命中 + 「资料中未找到相关内容」，不以低相关凑数 — verify: 无关提问 hits 为空且 message 匹配

## 8. 问答

- [ ] 8.1 实现 `POST /api/ask`：本班 hybrid 取前 4 条有依据切片，服务端写 system，组装标题/序号/正文带编号 — verify: 桩对话网关记录的输入含编号与切片正文，且无向量分量
- [ ] 8.2 有依据时返回 `{answer, citations}`，`[n]` 与 citations 同序；越界引用做校验 — verify: 桩返回含 `[1][2]`，citations 顺序一致
- [ ] 8.3 无命中时不调用对话网关，返回固定文案、`citations` 为空；丢弃客户端 system 消息 — verify: 无关提问时桩网关零调用；携带 client system 被忽略
- [ ] 8.4 更新 README/接口文档补充 `/api/search`、`/api/ask` 字段与示例 — verify: 文档示例可直接复现

## 9. 重建与一致性

- [ ] 9.1 实现 `GET /api/materials/{id}/vectors`（切片与向量对照，含 `index_status`、维度、payload 标识）— verify: 返回字段与 design §14 一致且仅本班
- [ ] 9.2 实现 `POST /api/materials/{id}/reindex`：先删旧切片与旧向量，再按本次策略重切、重嵌、重写 — verify: 重建后旧 point 不残留，切片数随策略变化
- [ ] 9.3 实现可重复执行的补嵌命令，只处理 `pending`/`failed` 或指定材料；重复执行幂等 — verify: 连续执行两次结果一致、无重复 point

## 10. 数据迁移（SQLite → MySQL）

- [ ] 10.1 实现一次性迁移子命令：读旧 SQLite 的 `classes/users/materials/knowledge_entries`，映射 `/app/uploads` 路径并校验文件存在 — verify: 迁移后 MySQL 行数与旧库一致，路径可读
- [ ] 10.2 迁移后对已有正文触发重建（切片+嵌入+写 Qdrant）— verify: 迁移材料在 keyword 与 vector 检索中均可命中
- [ ] 10.3 写迁移说明（来源、映射规则、回滚：保留旧库副本）— verify: 按文档从旧库副本可在干净 MySQL 上重跑

## 11. Compose 与健康检查

- [ ] 11.1 编写 `docker-compose.yml`：`web`(Nginx) + `api`(Go) + `db`(MySQL) + `qdrant`；MySQL 不映射宿主端口；Qdrant/网关不作为浏览器入口；`--ngram-token-size=2` — verify: `docker compose up --build -d` 后四容器 running，宿主不可直连 MySQL
- [ ] 11.2 配置 Nginx：静态资源 + `/api` 反代到 Go；上传目录不被暴露为静态目录 — verify: 浏览器仅经 Nginx 访问 `/api`，直请上传路径不可得
- [ ] 11.3 实现 `GET /health`（无需认证、不返回材料内容）并配置 healthcheck — verify: 未登录 `curl -sf /health` 返回 200 `{"status":"ok"}`
- [ ] 11.4 数据卷持久化：MySQL 数据与上传目录 mount；`down` 再 `up` 不丢数据 — verify: 上传一条后 down/up，列表与切片仍在
- [ ] 11.5 更新 README：Compose 启动、必需环境变量、登录 URL、`/health`、四服务拓扑 — verify: 按 README 从零可打开登录页并 curl health

## 12. 集成验收（第 3 课基线与第 4 课新场景）

- [ ] 12.1 重跑第 3 课场景：教师/学生登录、学生上传 403、跨班按 ID 404、上传后本班列表可见 — verify: 记录四条实测结果
- [ ] 12.2 第 4 课检索场景：keyword/vector/hybrid 三模式、班级隔离（注入 class_id 无效、跨班空命中）、可追溯字段、无依据不编造 — verify: 逐条执行 `knowledge-retrieval` 场景并记录结果
- [ ] 12.3 问答场景：有依据带 `[1][2]`、无依据不调用模型且 citations 为空 — verify: 记录实测（含桩/真实网关调用计数）
- [ ] 12.4 `openspec validate add-traceable-vector-retrieval --strict` 通过 — verify: exit 0 且无 error
