# Proposal

## Why

CampusClaw 第 3 课课件要求 **Go 服务 + Nginx + MySQL + 服务端会话**，而本仓库现有 `auth-upload` 以 Flask + SQLite + 客户端签名会话实现，与课件基线不一致。第 4 课要在「同一套系统」上增加**可追溯知识库检索**：把材料正文切分为切片、向量写入 Qdrant，支持关键字 / 向量 / 混合三种检索，并在有依据时生成带出处标注的简短回答；没有依据时明确返回未找到，不得编造。因此本变更先补齐第 3 课基线，再实现第 4 课检索与问答。

## What Changes

- **BREAKING：拉齐第 3 课基线。** Web 层由 Flask 改为 **Go**；数据库由 SQLite 改为 **MySQL**；会话由客户端签名 cookie 改为**服务端会话**（`sessions` 表 + Cookie 仅存随机标识，登录成功换发新标识）；增加 **Nginx** 反代 `/api`；接口统一到 `/api/*`；核心表收敛为 `classes / users / sessions / materials / knowledge_entries` 五张（均带 `class_id` 租户列的材料与知识库表）。
- **新增切片与入库：** 上传成功后按策略把 `knowledge_entries.body_text` 切分为 `knowledge_chunks`（正文、序号、字符区间、状态）；支持 `auto` / `custom` / `hierarchy` 三种策略与预处理，默认 `auto`（约 800 字、重叠 80 字）。
- **新增向量化：** 服务端调用外部嵌入网关，把每个切片转为固定维度向量，按切片主键**幂等**写入 Qdrant（collection `campusclaw_chunks`，余弦度量，payload 仅含标识、不含正文）。
- **新增检索：** `POST /api/search` 支持 `keyword`（MySQL FULLTEXT ngram，token=2）、`vector`（Qdrant，余弦阈值 0.35）、`hybrid`（两路各自过滤后按 RRF `k=60` 融合，默认）；命中至少含材料标题、切片序号、字符区间、摘录，并支持打开对应材料。
- **新增问答：** `POST /api/ask` 先做本班混合检索，取前 4 条有依据切片，带编号与正文调用对话网关，回答以 `[1]`、`[2]` 标注出处；无命中时**不调用模型**，返回固定文案、`citations` 为空。
- **新增重建路径：** 提供可重复执行的补嵌 / 重建（`POST /api/materials/{id}/reindex` 等），切换策略时先删旧切片与旧向量再重建。
- **保持隔离与安全：** 班级仅取自会话，关键字与向量两条路径均按会话班级过滤，回表取正文时再次核对；请求体中携带的 `class_id` 一律丢弃；跨班检索对外表现为空命中；跨班按 ID 访问材料仍为 404；登录失败原因对外统一；密钥与网关配置仅经环境变量注入。
- **文档与编排：** 更新 `README`、`docker-compose.yml`、`.env.example`，加入 MySQL、Qdrant、Nginx、嵌入与对话网关配置；`/health` 不要求认证、不返回材料内容。

## Capabilities

### New Capabilities

- `knowledge-retrieval`: 正文切片策略、切片与向量的入库、关键字 / 向量 / 混合三种检索、命中可追溯、无依据不编造、以及 `POST /api/ask` 的取据与引用标注行为。

### Modified Capabilities

- `auth-upload`: 由 Flask/SQLite 客户端签名会话基线，改为 Go + Nginx + MySQL 服务端会话基线（`sessions` 表、登录换发会话标识、`/api/*` 路由、五张核心表、Compose 编排）；材料上传在事务提交后保留 `knowledge_entries.body_text` 作为原文来源。

## Impact

- **服务**：新增 Go 服务与 Nginx 容器；替换现有 Flask 应用与 `requirements.txt`/`run.py` 入口。
- **数据**：新增 `sessions`、`knowledge_chunks` 表；`knowledge_entries.body_text` 保留为原文来源；`materials` 等业务数据需从现有 SQLite 迁移至 MySQL。
- **依赖服务**：新增 MySQL、Qdrant、外部嵌入网关与外部对话网关（地址、密钥、模型、维度经环境变量配置）。
- **接口**：新增 `POST /api/search`、`POST /api/ask`、`GET /api/materials/{id}/vectors`、`POST /api/materials/{id}/reindex`；既有登录 / 材料接口迁移到 `/api/*`。
- **验收**：第 3 课既有场景（登录、学生上传 403、跨班 404、上传后本班可见）需在新栈上重新执行；第 4 课新增检索、隔离、可追溯与问答场景。

## Non-goals（非目标）

- **不做流式长对话 / 会话编排**：流式输出、多轮会话管理属第 5 课。
- **不引入检索编排框架**：不使用 LangChain、LlamaIndex、Dify 等；由 Go 服务自行完成切分、嵌入与检索。
- **不做交叉编码器重排序**：本课不实现 rerank。
- **不引入消息队列 / 分布式事务**：两库一致性以「MySQL 为事实来源 + 幂等 upsert + 重建路径」保证，不引入 MQ。
- **不自部署嵌入 / 对话模型**：仅调用外部网关。
- **不做多租户 / 多校 / SSO**：班级隔离即本课边界。
- **不做 PDF / Word / 图片解析**：仍限 `.txt` / `.md`。
