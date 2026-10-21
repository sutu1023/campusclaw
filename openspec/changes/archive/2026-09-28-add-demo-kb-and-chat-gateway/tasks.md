# Tasks

## 1. 对话网关复用同一 API

- [x] 1.1 在 `config.Load` 中实现回退：`CHAT_BASE_URL` 为空时取 `EMBEDDING_BASE_URL`，`CHAT_API_KEY` 为空时取 `EMBEDDING_API_KEY`；`CHAT_MODEL` 默认 `course-chat`，显式值优先 — verify: 新增 `internal/config` 单元测试覆盖「仅设嵌入变量→对话地址与密钥等于嵌入值」「显式设置→使用显式值」「`CHAT_MODEL` 默认值」
- [x] 1.2 更新 `.env.example`，说明对话网关可回退到嵌入网关并列出 `CHAT_MODEL`（保持占位、无真实密钥）— verify: `grep -n "CHAT" .env.example` 含回退说明且 `git grep -n "API_KEY=." .env.example` 无真实值
- [x] 1.3 在 README 增加对话网关配置说明（只配 `EMBEDDING_*` 即可支持问答）— verify: 按文档仅设嵌入变量后，`/api/ask` 在有依据时返回回答而非 503

## 2. 演示知识库种子

- [x] 2.1 在 `store.Seed` 增加演示语料：A/B 两班各 ≥ 2 篇（设计为各 3 篇），正文充实（多段、含班级专属词），标题含「A 班」/「B 班」— verify: 集成测试断言两班各 ≥ 2 篇演示材料且 `knowledge_entries.body_text` 非空
- [x] 2.2 保证种子幂等：按 `(class_id,title)` 查存在再插入，正文不重复插入 — verify: 连续执行两次 `Seed`，`materials` 与 `knowledge_entries` 计数不变
- [x] 2.3 演示语料覆盖三种检索可命中：正文含 ≥2 字关键词，且可被同义问句向量命中 — verify: 集成测试用演示关键词走 keyword、用同义问句走 vector，均返回命中

## 3. 启动时确保索引

- [x] 3.1 实现「确保索引」遍历：对没有 ready 切片的材料执行 `Indexer.IndexMaterial`，在后台 goroutine 执行，不阻塞启动 — verify: 集成测试在种子后运行该遍历，演示材料出现 `index_status='ready'` 的切片；`/health` 不受影响
- [x] 3.2 嵌入网关不可用时降级：遍历失败不阻断启动，切片保留 pending/failed，并可由 `cmd/backfill` 补齐 — verify: 关闭嵌入桩后启动成功；恢复后用 `go run ./cmd/backfill` 将切片补为 ready
- [x] 3.3 在 README/`docs/MIGRATION.md` 记录补嵌与重建路径（`cmd/backfill`、`POST /api/materials/{id}/reindex`）— verify: 按文档命令可把 failed 切片补成 ready

## 4. 示例问句

- [x] 4.1 新增 `GET /api/examples`（需登录）返回示例问句列表，每项含文本与 `no_evidence` 标记，至少 3 条且含 1 条无依据 — verify: 集成测试断言未登录 401、登录后返回 ≥3 条且恰有 1 条 `no_evidence=true`
- [x] 4.2 SPA 在检索与问答界面拉取并展示示例问句，点击即以该问句发起查询 — verify: 组件测试或手工验证：点击示例后查询输入与请求体使用该问句
- [x] 4.3 无依据示例问句走通「未找到」路径 — verify: 集成测试用无依据示例调用 `/api/ask`，`citations` 为空且对话网关调用计数为 0

## 5. 演示验收与文档

- [x] 5.1 在 README 记录样例查询与预期（检索模式、预期命中材料、是否有出处、无依据样例）— verify: 按文档逐条复现，结果与预期一致
- [x] 5.2 端到端验收：种子 → 确保索引 → keyword/vector/hybrid 三模式命中 → 问答带引用 → 无依据不调用网关 → 跨班专属词无命中 — verify: 记录每一步实测结果（含网关调用计数）
- [x] 5.3 `openspec validate add-demo-kb-and-chat-gateway --strict` 通过 — verify: exit 0 且无 error
