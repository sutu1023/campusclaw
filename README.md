# CampusClaw

CampusClaw 是一个面向中小学教学与教研场景的智能平台，利用大语言模型、知识库等技术，
辅助教师完成备课、学情分析、教研问答等工作。

## 本学期目标

- 用户认证、角色权限与班级隔离（token 鉴权）
- 教学材料上传与知识库入库（切片 + 向量）
- 可追溯的知识库检索（关键字 / 向量 / 混合）
- 基于知识库的问答与出处标注
- 智能体（角色提示词 + 技能 / 材料绑定）与技能文件解析
- 多轮会话与 SSE 流式回答（含引用列表先行、命中技能展示）
- 由处理器注解生成的 OpenAPI 与 Swagger 页面
- 独立 SPA 前端（Vue 3）与北大红主题
- Docker Compose 一键启动与运行验证

## 本学期暂不做

- 交叉编码器重排序、语义路由（技能路由为确定性词法计分）
- WebSocket、断线续传等双向实时通信（单向问答用 SSE）
- Agent Skills 规范的 `SKILL.md` / YAML frontmatter（本课为自定义中文键格式）
- 消息队列 / 分布式事务；两库一致性以「MySQL 事实来源 + 幂等重建」保证
- 自部署嵌入 / 对话模型（仅调用外部网关）
- 多租户 / 多校 / SSO；JWT / 刷新令牌（令牌为服务端可撤销的不透明随机值）
- 服务端渲染 / 同构渲染
- PDF / Word / 图片解析（仅 `.txt` / `.md`）

## 架构

```
Browser ──> web (Nginx: 托管 SPA + 反代 /api) ──> api (Go) ──> db (MySQL)
                                                        └──────> qdrant
                                                        └──────> 外部嵌入 / 对话网关
```

- 浏览器只访问 Nginx；MySQL 不映射宿主端口；Qdrant 与模型网关仅由 Go 服务访问。
- 鉴权为 **token 方式**：登录签发高熵不透明令牌，客户端在
  `Authorization: Bearer <token>` 中携带，服务端 `sessions` 表只存令牌哈希
  （HMAC-SHA256），登出即时吊销。不使用 Cookie。

## 快速开始（Docker Compose）

前置条件：Docker 与 Docker Compose v2。

```bash
# 1. 准备环境变量
cp .env.example .env
# 生成并填入 TOKEN_SECRET（必填，缺失时应用拒绝启动）
python -c "import secrets; print(secrets.token_hex(32))"
# 如使用真实模型，填入 EMBEDDING_* 与 CHAT_* 网关配置

# 2. 构建并启动（首次启动自动建表与写入种子数据）
docker compose up --build -d
```

启动后访问：

- 应用（SPA）：<http://localhost:8080/>
- 登录页：<http://localhost:8080/login>
- 健康检查：<http://localhost:8080/health>（无需认证，返回 `{"status":"ok"}`）

### 预置账号

| 用户名 | 角色 | 班级 | 密码 |
| --- | --- | --- | --- |
| `teacher_a` | teacher | 班级 A | `CampusClaw#2026` |
| `student_a1` | student | 班级 A | `CampusClaw#2026` |
| `student_b1` | student | 班级 B | `CampusClaw#2026` |

密码以 bcrypt 哈希存储；`sessions` 表不保存明文令牌。

### `.env` 变量

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `TOKEN_SECRET` | 是 | 无 | 令牌哈希密钥（HMAC-SHA256） |
| `DB_DSN` | 是 | 无 | MySQL DSN（`...?parseTime=true`） |
| `MYSQL_ROOT_PASSWORD` / `MYSQL_USER` / `MYSQL_PASSWORD` / `MYSQL_DATABASE` | 否 | `root` / `campusclaw` | MySQL 容器引导 |
| `SESSION_TTL` | 否 | `24h` | 令牌有效期 |
| `MAX_UPLOAD_BYTES` | 否 | `16777216` | 上传上限 |
| `UPLOAD_DIR` | 否 | `/app/uploads` | 上传目录（仅挂载到 api） |
| `QDRANT_URL` / `QDRANT_COLLECTION` | 否 | `http://qdrant:6333` / `campusclaw_chunks` | 向量库 |
| `EMBEDDING_BASE_URL` / `EMBEDDING_API_KEY` / `EMBEDDING_MODEL` / `EMBEDDING_DIM` | 否 | 空 / 空 / `text-embedding-3-small` / `1536` | 嵌入网关 |
| `CHAT_BASE_URL` / `CHAT_API_KEY` / `CHAT_MODEL` | 否 | 空 / 空 / `course-chat` | 对话网关（地址与密钥留空时回退到嵌入网关） |
| `VECTOR_MIN_SCORE` / `RRF_K` | 否 | `0.35` / `60` | 检索参数 |

### 对话网关复用嵌入网关（同一 API）

只配置 `EMBEDDING_BASE_URL` / `EMBEDDING_API_KEY` 即可同时支持向量化与问答：
当 `CHAT_BASE_URL` / `CHAT_API_KEY` 留空时，服务端自动回退使用嵌入网关的地址与密钥；
`CHAT_MODEL` 指定该网关上的对话模型（默认 `course-chat`）。显式配置的对话网关始终优先。
网关地址与密钥只在服务端使用，不下发浏览器。

## API

除 `POST /api/login` 与 `GET /health` 外，所有 `/api/*` 需要
`Authorization: Bearer <token>`；令牌缺失、无效或过期统一返回 `401`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/login` | `{username,password}` → `{token,user}` |
| POST | `/api/logout` | 吊销当前令牌 |
| GET | `/api/me` | 当前用户 |
| GET | `/api/materials?page&page_size&title` | 本班分页列表 |
| POST | `/api/materials` | multipart：`file,title,chunk_*` |
| GET | `/api/materials/{id}` | 材料详情（含 `body_text`） |
| GET | `/api/materials/{id}/vectors` | 切片与向量对照 |
| POST | `/api/materials/{id}/reindex` | 按新策略重建（先删旧切片与向量） |
| POST | `/api/search` | `{q,mode,limit,offset}` |
| POST | `/api/ask` | `{q,history}` |
| GET | `/api/agents` | 本班智能体（学生仅已启用） |
| POST | `/api/agents` | 教师新建智能体（含 `skill_ids`、`material_ids`） |
| GET | `/api/agents/{id}` | 读取一个智能体 |
| PUT | `/api/agents/{id}` | 教师修改智能体与绑定 |
| GET | `/api/skills` | 本班技能 |
| POST | `/api/skills` | 教师用技能文件新建技能 `{source}` |
| PUT | `/api/skills/{id}` | 教师改写技能 |
| DELETE | `/api/skills/{id}` | 教师删除技能（同时清绑定） |
| GET | `/api/conversations` | 我的会话（按最近提问倒序） |
| POST | `/api/conversations` | 按本班已启用智能体新建会话 `{agent_id}` |
| GET | `/api/conversations/{id}` | 会话与消息（含 `citations`、`skill_name`） |
| POST | `/api/conversations/{id}/messages` | 在会话中提问，`text/event-stream` 流式返回 |
| GET | `/openapi.json` | OpenAPI 描述（无需认证） |
| GET | `/swagger/index.html` | Swagger UI（无需认证） |
| GET | `/health` | 无需认证 |

### 接口文档（OpenAPI / Swagger）

文档由处理器注解生成，与实现同源。安装 `swag` 后重新生成：

```bash
make docs          # 等价于: swag init -g cmd/api/main.go -o docs --parseInternal
```

生成物提交在 `docs/`（`docs.go`、`swagger.json`、`swagger.yaml`），随后：

- 浏览器打开 <http://localhost:8080/swagger/index.html> 可对照文档试调接口；
- `curl http://localhost:8080/openapi.json` 返回 OpenAPI 描述。

### 智能体、技能与会话

- 智能体是本班的配置单位：一份角色提示词 + 绑定的技能与本班材料。教师可创建/修改/启停；
  学生只能列出并选用**已启用**的智能体。绑定他班资源按不存在处理（404）。
- 技能以单一文本文件定义：空行以上为元数据（`名称`/`适用条件`/`资料`/`工具`/`启用`），
  空行以下为引导正文；`工具` 只接受 `knowledge_search`。解析失败返回可读的 400。
- 新建会话时必须选定本班一个已启用智能体，之后该会话固定使用它（换智能体请新开会话）。

### SSE 流式问答

`POST /api/conversations/{id}/messages` 以 `text/event-stream` 返回四类事件：

```
event: meta
data: {"status":"ok","skill":{"name":"数学解题引导"},"citations":[...]}

event: delta
data: {"text":"先看这一步"}

event: done
data: {"message_id":42}
```

- `meta` 最先到达，携带本次命中的技能与**全部**引用；`delta` 是新增文本（拼接等于最终回答）；
  `done` 在助手消息落库后返回编号；流开始后失败只能以 `error` 事件收尾。
- 检索只在会话所绑定智能体的材料内进行，取前 4 条；命中「解题引导」类技能时不写得数；
  检索落空仍作答，引用为空且正文不带 `[n]`。
- Nginx 侧对流式路径关闭 `proxy_buffering` / `proxy_cache` 并放宽读超时，服务端返回
  `X-Accel-Buffering: no`，构成两层防护。

### 示例：登录与带令牌检索

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"teacher_a","password":"CampusClaw#2026"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

curl -s -X POST http://localhost:8080/api/search \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"q":"勾股定理","mode":"hybrid"}'
```

### 示例：教师上传

```bash
curl -s -X POST http://localhost:8080/api/materials \
  -H "Authorization: Bearer $TOKEN" \
  -F 'title=第三课教案' -F 'file=@lesson.md' \
  -F 'chunk_strategy=auto'
```

上传参数（可选）：`chunk_strategy`（`auto`/`custom`/`hierarchy`）、
`chunk_separator`、`chunk_max_chars`（100–2000）、`chunk_overlap_percent`（0–50）、
`chunk_collapse_ws`、`chunk_strip_links`。返回：
`{material_id,title,chunk_strategy,chunk_count}`。

### 检索与问答

- `POST /api/search`：`mode` 取 `keyword` / `vector` / `hybrid`（默认）。
  命中含 `chunk_id`、`material_title`、`chunk_index`、`start_offset`、
  `end_offset`、`snippet`；无依据时 `status="no_evidence"`、
  `message="资料中未找到相关内容"`。
- `POST /api/ask`：先做本班 hybrid 检索，取前 4 条有依据切片；
  有依据时返回 `{answer,citations}`，`[n]` 与 `citations` 同序；
  无依据时不调用对话网关，返回固定文案且 `citations` 为空。

## 演示知识库与示例问句

种子脚本预置一份可直接检索与问答的演示知识库：班级 A 与班级 B 各 3 篇教学/教研材料，
正文含班级专属词（A：`青竹`，B：`红叶`）用于演示班级隔离。启动时服务端会在**后台**为尚无
ready 切片的材料补齐切片与向量；若嵌入网关暂时不可用，启动不会失败，可稍后执行补嵌命令。

`GET /api/examples` 返回示例问句（需登录），检索与问答界面会展示并可一键发起查询。

### 样例查询与预期

| 会话 | 模式 | 查询 | 预期 |
| --- | --- | --- | --- |
| 教师 A（班级 A） | keyword | `勾股` | 命中「A 班-勾股定理教案.md」 |
| 教师 A（班级 A） | vector | `勾股定理的内容是什么？` | 命中同一材料 |
| 教师 A（班级 A） | hybrid | `分数的意义是什么？` | 命中「A 班-分数教学教研笔记.md」 |
| 教师 A（班级 A） | keyword | `红叶` | 空命中（B 班专属词） |
| 教师 A（班级 A） | `/api/ask` | `课堂提问有哪些有效技巧？` | 返回回答且 `citations` 非空 |
| 教师 A（班级 A） | `/api/ask` | `今天天气怎么样？` | `status=no_evidence`、`citations` 为空、不调用对话网关 |

## 接口测试（apitest）

`apitest/` 用 Python 标准库实现，一个接口一个脚本；脚本路径按 URL 组织，例如
`cases/api/login/POST.py` 对应 `POST /api/login`，`cases/api/conversations/{id}/messages/POST.py`
对应 `POST /api/conversations/{id}/messages`。每个脚本声明 `ORDER` 并在 `run(ctx)` 中返回一组
`(断言描述, 是否通过)`；用例间通过 `ctx` 传递令牌与编号。

```bash
# 先启动服务（docker compose up -d 或本地 make build 后运行）
python3 apitest/run_all.py --base-url http://localhost:8080
```

- 除状态码外，还会**回读接口**（读的是 MySQL）核对写入内容，例如登录角色、智能体绑定、
  会话消息与 `delta` 拼接后的正文一致。
- 运行结果写入 `apitest/report/report.html`（自包含 HTML，**不提交 Git**，已在 `.gitignore` 中）。
- 覆盖登录、`/api/me`、材料列表、技能查询 / 新建、智能体查询 / 新建、检索、
  新建会话与发送一条流式消息。

## 补嵌与重建

- 补嵌（`pending`/`failed` → `ready`，可重复执行）：
  `DB_DSN=... EMBEDDING_BASE_URL=... go run ./cmd/backfill`
- 重建单份材料（先删旧切片与旧向量，再按新策略重切重嵌）：
  `POST /api/materials/{id}/reindex`，请求体形如
  `{"chunk_strategy":"custom","chunk_max_chars":500,"chunk_overlap_percent":10}`。
- 更换演示语料：种子不覆盖已存在的正文；删除数据卷重来，或对相关材料执行重建。

## 权限与班级隔离约定

- 未登录或令牌无效访问受保护 API 返回 `401`。
- 学生调用上传/重建接口返回 `403`，且不产生数据库行或落盘文件。
- 跨班按 ID 访问材料统一返回 `404`（不区分「不存在」与「无权限」）。
- 班级只取自令牌对应的服务端会话；请求体/参数/头中的 `class_id` 一律丢弃。
- 所有业务查询按会话 `class_id` 过滤；向量命中回表时再次核对班级。

## 前端开发与构建

前端为独立 SPA（Vue 3 + Vite + TypeScript），位于 `web/`：

```bash
cd web
npm ci
npm run dev        # http://localhost:5173，/api 代理到 localhost:8080
npm run build      # 产物输出到 web/dist
npm test           # vitest：API 客户端 401 处理与请求头注入
npm run lint
```

### 主题色板

北大红主题在 `web/src/theme.css` 中集中定义：主色 `--pku-red: #94070A`，
辅助色 `--pku-red-alt: #BE2126`，并派生 hover 与浅背景。主色用于按钮、链接与
强调；正文使用近黑以适应 WCAG AA 对比度。

## 数据迁移

旧 SQLite 数据迁移到 MySQL 的步骤见 [`docs/MIGRATION.md`](docs/MIGRATION.md)。

## 开发方式

本项目采用 SDD（Specification-Driven Development，规约驱动开发）：

> 无规约，不写代码。

需求先形成可执行、可验收的规约，再根据规约进行开发和验收。
