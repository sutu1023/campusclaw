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
