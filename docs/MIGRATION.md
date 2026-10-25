# 数据迁移：SQLite → MySQL

本文件对应 `tasks.md` 10.1–10.3。迁移把旧的 Flask + SQLite 数据搬到新的
Go + MySQL 基线，并对已有正文重建切片与向量。

## 来源

- 旧库：`data/app.db`（SQLite）。相关表：`classes`、`users`、`materials`、
  `knowledge_entries`。
- 旧上传目录：容器内 `/app/uploads`，本机 `./uploads`。

## 运行

```bash
# 目标 MySQL 必须先就绪（可用 docker compose up -d db qdrant）
export DB_DSN='campusclaw:campusclaw@tcp(127.0.0.1:3306)/campusclaw?parseTime=true'
export UPLOAD_DIR=./uploads
export EMBEDDING_BASE_URL=... EMBEDDING_API_KEY=... EMBEDDING_DIM=1536

go run ./cmd/migrate -sqlite ./data/app.db
```

迁移后可用补嵌命令重试失败/待处理的切片：

```bash
go run ./cmd/backfill -status pending,failed
```

也可以对单份材料重建索引（先删旧切片与旧向量，再按新策略重切重嵌）：

```bash
# 经 API（需教师令牌）
curl -X POST http://localhost:8080/api/materials/<id>/reindex \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"chunk_strategy":"custom","chunk_max_chars":500,"chunk_overlap_percent":10}'
```

## 映射规则

- `classes` / `users`：按主键 upsert，保留原 id；bcrypt `$2` 哈希可直接被 Go
  `golang.org/x/crypto/bcrypt` 校验。
- `materials`：保留原 id 与标题；`file_path` 取 `/uploads/` 之后的相对路径，
  拼到当前 `UPLOAD_DIR`；若新路径不存在则回退旧路径。
- `knowledge_entries.body_text`：原样保留为原文来源。
- 每个材料执行一次重建：重新切片 → 重新嵌入 → 幂等写入 Qdrant。
  种子材料无正文时不产生切片。

## 回滚

- 迁移只读取旧 SQLite，不修改它；保留 `data/app.db` 与 `./uploads` 副本即可回滚。
- MySQL 侧数据可重建：清空 MySQL 与 Qdrant 后重跑 `cmd/migrate` 即可。
- 不引入迁移框架，也不保留 SQLite 作为运行时存储。

## 第 5 课 MySQL → MCP 扩展升级（不清库）

本节不同于上面的旧 SQLite 导入：已有第 5 课 MySQL **不得清空或删除数据卷**。
先备份数据库与上传文件，并在独立副本验证升级；不要对生产 DSN 运行任何集成测试。
例如把备份写到仓库外（备份包含用户数据，不提交 Git）：

```bash
docker compose exec -T db sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" exec mysqldump -u"$MYSQL_USER" --single-transaction --no-tablespaces "$MYSQL_DATABASE"' > ../campusclaw-before-mcp.sql
```

升级 API 时 `Store.Migrate` 自动、幂等执行：

1. 创建 `mcp_servers`、`mcp_tools`、`mcp_discovery_snapshots`、`mcp_discovery_changes`、
   `mcp_tool_calls`、`mcp_conversation_usage` 六张表及班级/时间/提问索引。
2. 查询 `information_schema.columns`，把旧 `skills.tools_json VARCHAR(255)` 扩为 `TEXT`；
   已经是 TEXT/MEDIUMTEXT/LONGTEXT 时不缩小，完整保留现有工具、技能文件和绑定。
3. 仅在缺少列时为 `messages` 增加可空 `tool_calls_json MEDIUMTEXT`；旧行保持 NULL，
   读取时映射为 `[]`，不补造调用事件。消息 role 仍仅为 user/assistant。

迁移不改变材料、会话归属、原消息正文或末尾 6 条/每条最多 500 字的历史窗口，
中间 MCP assistant/tool 协议消息也不作为独立消息写入。重复启动不会重复加列或清空历史。
断开/启动连接失效只撤销可用目录，保留批准快照、变更记录、调用审计和累计用量。

### 独立测试库验证

`TEST_DB_DSN` 必须指向可丢弃的独立 MySQL 数据库，`TEST_QDRANT_URL` 指向测试 Qdrant；
Store/HTTP 测试会按外键依赖顺序 DROP 并重建全部业务表。不要指向应用数据卷中的业务库。
使用同一个测试 DSN 时须序列执行包，避免两个包互相重建数据库：

```bash
# 先在当前 shell 安全配置上述专用 TEST_* 变量，不在日志打印 DSN/凭据
go test -race -p 1 ./internal/store ./internal/httpapi
```

覆盖新库、第 5 课列形状升级、重复迁移、长工具列表、旧消息空集合、原材料/技能/会话/
历史窗口、最后一个额度的并发预留，以及审计写入失败的 fail-closed 行为。
故障测试使用临时 CHECK 约束，不要求 MySQL SUPER 权限或降低服务器全局安全配置。
没有配置测试环境时的 skip 不能记为验收通过。

### 应用回滚（保留扩展列与证据）

1. 在仍运行新 API 时断开所有 MCP 服务；把已含 MCP 引用的技能改回内置工具或停用，
   避免旧版本拒绝新引用语法。只停用而保留原文时，旧版本不应尝试重新保存这些文件。
2. 停止 `mcp-demo`，再回滚 API/web 的镜像版本。
3. 保留新增 MCP 表、TEXT 扩容列、可空摘要列和审计/额度；不要向下缩回 VARCHAR(255)，
   不执行 DROP、清卷或用备份覆盖仍有新数据的业务库。旧应用可忽略新增列/表。
4. 检查原材料、旧技能、会话与消息仍可读取；重新升级后教师须重新连接 MCP，
   不允许通过回滚/重启重置累计额度或绕过说明变更确认。
