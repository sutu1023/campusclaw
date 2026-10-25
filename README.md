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
                                                        └──────> mcp-demo:8090（同仓教学 MCP）
```

- 浏览器只访问 Nginx；MySQL 与 MCP 不映射宿主端口；Qdrant、模型网关与 MCP 仅由 Go 服务访问。
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
# 再生成独立随机值填入 MCP_AUTH_TOKEN（启动 mcp-demo 时必填）
# 按真实班级编号检查 MCP_CREDENTIAL_BINDINGS；不要复用 TOKEN_SECRET 或网关密钥
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
| `MCP_AUTH_TOKEN` | 仅 MCP 服务必填 | 空 | 服务端 MCP Bearer；不传入浏览器或构建参数 |
| `MCP_CREDENTIAL_BINDINGS` | 否 | 空 | 部署批准的班级/服务器/origin/凭据变量 JSON；空值不阻断 API 启动 |
| `MCP_MAX_ROUNDS` | 否 | `3` | 每问工具决策轮数（1–64） |
| `MCP_MAX_CALLS_PER_TURN` | 否 | `8` | 每问调用尝试数，含参数错误和拒绝（1–64） |
| `MCP_MAX_CALLS_PER_CONVERSATION` | 否 | `40` | 每会话持久化远端动作额度，成功/失败均消费（1–10000） |
| `MCP_TURN_TIMEOUT` | 否 | `20s` | 工具阶段总预算，含模型决策（正 Go duration，最大 2m） |
| `MCP_REQUEST_TIMEOUT` | 否 | `5s` | 单次 MCP 请求超时（正 Go duration，最大 1m） |

### 对话网关复用嵌入网关（同一 API）

只配置 `EMBEDDING_BASE_URL` / `EMBEDDING_API_KEY` 即可同时支持向量化与问答：
当 `CHAT_BASE_URL` / `CHAT_API_KEY` 留空时，服务端自动回退使用嵌入网关的地址与密钥；
`CHAT_MODEL` 指定该网关上的对话模型（默认 `course-chat`）。显式配置的对话网关始终优先。
网关地址与密钥只在服务端使用，不下发浏览器。

### MCP 部署批准与凭据边界

真实 MCP 密钥仅填写在本机、未跟踪的 `.env` 的 `MCP_AUTH_TOKEN` 中；API 和独立
MCP 服务在运行时读取同一值，不把它写进 Dockerfile、镜像构建参数、`VITE_*`、技能或浏览器。
教师登记的是服务器名称与地址，不是密钥；公共接口仅用 `has_token` 表示是否配置。

部署方另设**不含密钥**的批准表，例如（按真实班级编号调整）：

```dotenv
MCP_CREDENTIAL_BINDINGS='[{"class_id":1,"server_name":"campus-tools","base_url":"http://mcp-demo:8090","token_env":"MCP_AUTH_TOKEN"}]'
MCP_AUTH_TOKEN=
```

- 凭据必须同时匹配会话班级、服务器名称与规范化 origin；同班另一个名字/地址也不能借用。
  `token_env` 只能是启动时批准的 `MCP_*_TOKEN`（或 `MCP_TOKEN`）变量，不能查询任意环境变量。
  每个批准目标使用独立凭据变量；配置中不允许复用同一变量。
- Base URL 只允许 HTTP(S) origin，接受单个根目录尾斜线，统一主机名大小写和默认端口；
  拒绝用户信息、非根路径、query、fragment。客户端不跟随重定向，避免带密钥跳往其他目标。
- `http://mcp-demo:8090` 是运维明确批准的 Compose 内网目标，**不是**对任意私网、
  本机或第三方 URL 放行。只有匹配批准表后才会读取对应凭据并创建客户端。
- 不设置绑定/密钥不会阻断主 API 启动，但不能使用 MCP；已批准但缺少密钥显示未配置。
  独立 MCP 服务不允许无令牌启动。非法绑定、非正数/超大配额及非法 duration 会使 API 配置校验失败。
- 工具发现最多 64 项，说明最多 2000 字符、Schema 最多 64 KiB；响应最多 1 MiB。
  参数必须是最多 16 KiB、JSON 深度不超过 16 的对象，禁止重复键。
  固定离线 JSON Schema 校验器只支持对象、字符串、数值/整数、布尔、null，及
  `properties`/`required`/布尔 `additionalProperties`、标量 `enum`、长度/数值约束和 `description`。
  不支持数组、引用（包括本地/远程 `$ref`）、自定义方言、正则或组合 Schema，发现时即拒绝；不会加载网络/文件引用。

### 独立 MCP 服务验证（不依赖模型）

同仓 `mcp-server/` 使用根 Go module；API 与 MCP Dockerfile 均从仓库根目录构建，
共用排除所有层级 `.env`/`.env.*` 的 `.dockerignore`。密钥只在 Compose 运行时注入，
不会作为 `ARG`、Dockerfile `ENV` 或前端构建变量使用。API 没有对 MCP 的 `depends_on`，
MCP 不可用也不妨碍 API 启动；只运行主应用时可用 `docker compose up -d db qdrant api web`。

准备好本机 `.env` 中的 `MCP_AUTH_TOKEN` 与批准绑定后执行（必要时仅在 Docker 命令前加 `sudo`）：

```bash
docker compose build api mcp-demo
docker compose up -d db qdrant api mcp-demo
docker compose ps mcp-demo
# 健康探测是二进制内建功能，退出码 0 表示成功；镜像无需 curl/shell
docker compose exec -T mcp-demo /mcp-server --healthcheck
# 从真实 API 容器初始化、完成通知、发现工具，并逐一调用两个只读教学工具
docker compose exec -T api /mcpcheck
# 仅对这个验证进程临时覆盖为故意错误的非密钥值，不修改服务或 .env
docker compose exec -T -e MCP_AUTH_TOKEN=deliberately-invalid-verification-token api /mcpcheck -expect-unauthorized
```

`/mcpcheck` 默认使用批准项 `class_id=1`、`campus-tools`、`http://mcp-demo:8090`；
可用 `-class-id`、`-server`、`-base-url` 选择其他**已批准**项，未批准目标不会出站。
成功输出包含 `health=ok`、`protocol=2025-03-26`、`discovered=2`，以及两个调用的
`tool/status/characters`，不打印密钥、完整工具结果或网络诊断；错误令牌用例输出
`authentication=unauthorized`。这是服务网络/协议验证，不是模型提参或对话编排验收。

协议范围明确限定为：单端点 `POST /mcp`、JSON-RPC 2.0、固定 MCP `2025-03-26`，
支持 `initialize` → `notifications/initialized` → `tools/list` / `tools/call`；通知返回空正文。
`GET /health` 公开返回 `{"status":"ok"}`。不提供 stdio、旧双通道 SSE、服务端主动通知、
resources/prompts、分页发现或完整可选 Streamable HTTP 会话能力。

- `formula_hint`：提示公式适用条件、符号检查和下一步，不计算练习最终数值。
- `unit_convert`：提示长度/质量/时间的关系、量纲和方向，不代算具体练习。
- 两者仅接收 `{"query":"问题"}`（1–500 字符且不能全空白，无额外字段），
  返回文本 `content` 和 `isError`；不写文件、不访问外部网络。
- `POST /mcp` 缺失/错误 Bearer 先返回 HTTP 401，不解析工具请求；缺少服务令牌时拒绝启动。
  已鉴权的协议错误使用 -32700/-32600/-32601/-32602，不伪装成成功的工具文本。

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
| GET / POST | `/api/mcp/servers` | 教师列出 / 登记本班批准目标（无密钥输入） |
| GET / PUT / DELETE | `/api/mcp/servers/{id}` | 教师读取 / 修改地址 / 软删除 |
| POST | `/api/mcp/servers/{id}/connect` | 握手与完整发现，connected 或 pending_approval |
| POST | `/api/mcp/servers/{id}/disconnect` | 幂等断开，保留登记与历史 |
| GET | `/api/mcp/servers/{id}/tools` | 完整可用目录、待确认/批准内容及变更标识 |
| POST | `/api/mcp/servers/{id}/approve` | 教师确认 `{snapshot_hash}`；过期返回 409 |
| GET | `/api/mcp/audit?page&page_size&server_id&status` | 本班审计倒序分页；status 为 ok/error/denied |
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

### MCP 教师管理约定

所有 MCP 管理、目录及审计接口都要求**应用登录 Bearer + teacher 角色**；学生一律 403，
按编号访问他班/不存在服务器均为 404，班级只从服务端会话取得。教师提交的
`{name,base_url}` 必须精确匹配部署批准表；名称创建后不可改，软删除后不允许同名接管。
PUT 只修改批准地址/绑定，变更立即清空可用与待确认目录，不自动连接；缺少凭据时仍可登记，
但连接返回可读 400。接口从不收集/返回真实 MCP 密钥，只提供 `has_token`。

连接先撤销旧许可，再执行 initialize/initialized/list；失败返回脱敏 503，并保持不可调用。
远端 401 **不是**应用身份失败，不应触发浏览器登出。发现清单整体替换，重复连接不会追加；
移除工具也会移除本地目录。发现由教师重连触发，不提供自动远端变更侦测。
已有工具的说明/Schema 改变时返回 `pending_approval`：`tools=[]`，`pending_tools` 和
`approved_tools` 提供完整比较内容，`change` 含前后 hash、工具名、时间及确认信息。
教师核查后提交当前 `snapshot_hash`；过期/并发确认或迟到连接返回 409。

断开幂等返回 204，清空可用/待确认内容，但保留批准比较历史；断开/重启不能绕过变化确认。
API 每次启动都撤销遗留连接，保留登记、审计和累计用量，必须重新连接。
审计每页默认 20、最大 100，按时间/id 倒序，可筛选本班已删除服务器的历史来源；
他班/不存在 `server_id` 筛选统一 404。摘要不含参数、完整提示词、原始远端正文或密钥。

### 接口文档（OpenAPI / Swagger）

文档由处理器注解生成，与实现同源。安装 `swag` 后重新生成：

```bash
make docs          # 等价于: swag init -g cmd/api/main.go -o docs --parseInternal
```

生成物提交在 `docs/`（`docs.go`、`swagger.json`、`swagger.yaml`），随后：

- 浏览器打开 <http://localhost:8080/swagger/index.html> 可对照文档试调接口；
- `curl http://localhost:8080/openapi.json` 返回 OpenAPI 描述。

真实浏览器试调脚本为 `apitest/swagger_browser.cjs`。在仓库外安装 Playwright 1.56.1 及
Chromium headless shell，并准备浏览器运行库后执行（不加入应用依赖）：

```bash
NODE_PATH=/path/to/external/node_modules \
  APITEST_BASE_URL=http://localhost:8080 node apitest/swagger_browser.cjs
```

脚本实际点击 Swagger 的 Try it out / Execute，仅使用应用登录 Bearer，不读取 MCP 密钥；
验证连接、完整工具目录、过期确认 409、技能保存/回读、审计查询和断开后拒绝保存。
复用已有固定登记并检查重复登记 400，不自动批准未知变更。可通过 `APITEST_TEACHER_USERNAME` /
`APITEST_TEACHER_PASSWORD` 指定测试教师；默认使用预置演示账号。不要对业务环境运行写入测试。
脱敏报告位于被忽略的 `apitest/report/swagger-browser.json`，不保存含 Bearer 的截图、HAR 或 trace。

### 智能体、技能与会话

- 智能体是本班的配置单位：一份角色提示词 + 绑定的技能与本班材料。教师可创建/修改/启停；
  学生只能列出并选用**已启用**的智能体。绑定他班资源按不存在处理（404）。
- 技能以单一文本文件定义：空行以上为元数据（`名称`/`适用条件`/`资料`/`工具`/`启用`），
  空行以下为引导正文；`工具` 接受空值、内置 `knowledge_search` 与 `tool@server`。
  MCP 工具名为 `[a-z][a-z0-9_]{0,63}`，服务器名为 `[a-z][a-z0-9_-]{0,31}`；
  支持中英文逗号、顿号、分号和竖线，按首次出现去重，最多 8 项，不支持未限定服务器的普通工具名。
  解析失败返回可读 400；保存前在同一写事务校验本班已连接、无待确认、工具存在于批准目录。
  不存在/他班服务器一致 404；断开/待确认/未知工具为带引用原因的 400，失败不改原文或绑定。
  内置或空工具不依赖 MCP；词法路由只携带命中技能的工具引用，不扩大候选集。
- 新建会话时必须选定本班一个已启用智能体，之后该会话固定使用它（换智能体请新开会话）。

例如，先连接并发现本班 `campus-tools`，再保存以下技能（文件和引用都不含凭据）：

```text
名称：公式与单位解题引导
适用条件：勾股,公式,单位
资料：数学
工具：knowledge_search,formula_hint@campus-tools,unit_convert@campus-tools
启用：是

先确认公式适用条件与单位是否统一，只提示下一步检查方法，不给最终得数。
```

保存后通过 `GET /api/skills` 可读回完整文件与引用；断开后原文件仍保留，但不能再次保存为
可用 MCP 引用，也不会自动重连。长引用列表使用 TEXT 存储，不受旧 VARCHAR(255) 限制。

教师登录后，导航中的 **MCP 工具** 页可登记/修改地址、连接/断开/删除，查看完整工具说明与
Schema；`has_token` 只显示“已配置/未配置”，没有密钥输入框。待确认时并排阅读上次批准与
新发现的完整定义和 hash，再显式确认当前快照；过期确认显示后端错误并刷新状态，不自动批准。
审计可按状态、服务器编号（含已删除登记）筛选并翻页，显示用户/会话/提问/轮次及安全摘要。
学生看不到管理入口，直接访问也会被路由拒绝，后端权限校验仍独立存在。

在 **技能** 页的教师编辑区可查看本班当前已连接工具的完整参考与 `tool@server` 写法，手工写入
原始技能文件后保存；可用参考不授予权限。断开/待确认后刷新即从参考移除，保存错误按后端原文
显示并保留正在编辑的文件。仅内置项和空工具不依赖 MCP。MCP 凭据不进入 SPA 状态或存储。

### 模型工具兼容与执行边界

工具编排复用现有 `CHAT_BASE_URL` / `CHAT_API_KEY` / `CHAT_MODEL` 及原配置回退规则，
不会擅自切换模型。网关需支持 OpenAI 兼容的 `tools` / `tool_choice`、非流式
`assistant.tool_calls` 和关联的 `tool_call_id`；最终请求使用 `stream=true`、
`tool_choice="none"`，必须真正返回 SSE 增量。发现说明与 Schema 原样作为函数声明，
稳定别名区分跨服务器同名工具；只包含命中技能的候选，不把本班整个目录交给模型。
MCP 地址、凭据引用和令牌不进入模型请求，网关认证只使用自己的密钥。

- 参数由模型提出，宿主不把 `query` 固定替换为原问题；使用声明所用的同一 Schema 校验。
  每次执行重新核对当前连接、generation、批准快照与部署绑定，并先提交审计/额度事务。
- 默认最多 3 轮决策、8 次/问尝试、40 次/会话已预留远端动作；参数错误/拒绝也占本问尝试，
  同批调用逐项计算，超出预算逐项拒绝并停止继续决策。会话额度跨提问/启动保留，失败不退款。
  每份决策响应最多 16 项，超大列表整体拒绝，不执行前半份。
- 工具阶段有独立 20s deadline，单次 MCP 请求默认 5s；取消后不再启动调用或最终生成，
  只允许已有调用做有界审计收尾。工具阶段耗尽预算不取消最终正文的父上下文。
- 结果以带 `tool@server` 来源的 `tool` 数据消息回传；调用和结果始终成对，宿主标识在
  轮次、SSE 与审计间一致。工具中间消息不会持久化为聊天历史，也不扩大 6 条/500 字窗口。
- 空/非文本/`isError`、超时、401、协议/体积错误均为局部 `error`；已知指令式载荷、
  MCP/网关真实密钥或目标地址回显整条拒用，不做局部删改后冒充成功。
  安全文本最多 2000 字进入模型，安全展示/审计摘要最多 500 字。

| 结果 | 含义 |
| --- | --- |
| `ok` | 真实远端调用、数据检查和审计终结均成功 |
| `error` | 参数错误、工具/协议/安全检查失败，或审计不可用；不会伪造成功 |
| `denied` | 引用/连接/批准快照/候选不允许，或轮次、尝试、会话、时间预算耗尽 |

工具决策的整段 `content` 不作为最终答案；停止调用后重新发起流式生成，不能拆分缓存答案
伪装进度。网关拒绝工具协议或返回非法结构时明确显示阶段错误，并恢复工具阶段之前的
可信正文请求：移除 `tools` / `tool_choice` 及中间协议，不把工具结果偷换为 system/user。
无 MCP 技能沿用一次原文本流请求，无额外决策开销；工具失败不改变 `/api/ask`。
确定性网关桩只证明协议与边界，不能代替真实网关两轮验收；具体证据见 `VERIFICATION.md`。

### SSE 流式问答

`POST /api/conversations/{id}/messages` 以 `text/event-stream` 返回四类事件：

```
event: meta
data: {"status":"ok","skill":{"name":"公式与单位解题引导"},"citations":[]}

event: meta
data: {"status":"ok","skill":{"name":"公式与单位解题引导"},"citations":[],"tool_calls":[{"call_id":"q41-c1","ref":"formula_hint@campus-tools","round":1,"status":"ok","text":"先核对公式适用条件"}]}

event: meta
data: {"status":"ok","skill":{"name":"公式与单位解题引导"},"citations":[],"tool_calls":[{"call_id":"q41-c2","ref":"unit_convert@campus-tools","round":2,"status":"ok","text":"再检查单位关系与换算方向"}]}

event: delta
data: {"text":"先看这一步"}

event: done
data: {"message_id":42}
```

- 首次 `meta` 最先到达，携带本次命中的技能与**全部**引用；后续 `meta` 保持它们不变，
  每次仅追加新增 `tool_calls`。客户端须按 `call_id` 合并去重，不能覆盖前几轮的记录。
  预先失效引用用第 0 轮 `denied`；调用错误是 `error`，外层 `meta.status` 始终为 `ok`。
  没有调用、拒绝或阶段错误时省略字段，模型认为无需调用时也不制造记录。
- 工具元数据全部先于首个 `delta`。`delta` 是新增文本（拼接等于最终回答），`done` 在助手
  消息落库后返回编号；正文、引用、技能名及工具摘要可通过读取会话回放，旧消息摘要为 `[]`。
  中间协议不作为独立消息落库，也不增加历史窗口。
- 聊天页在正文前显示“工具使用”栏，按轮次与首次发生顺序合并三种状态；重复 call_id 更新
  原记录，不新增重复项。刷新从消息恢复，没有工具时隐藏栏。材料标题、工具说明/结果均用
  Vue 文本插值转义，不使用 `v-html`。流内错误或刷新失败保留已收到的正文与工具状态，离开
  聊天页会中止当前 fetch，避免为已离开的页面继续调用或生成。
- 断开/超限/工具故障只改变调用状态，主回答继续。主路径尚无正文即失败时发送通用 `error`；
  已有部分正文时沿用原规则保存并 `done`，不回显上游诊断或丢弃已生成文本。
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
  新建会话与发送一条流式消息，以及 MCP 登记/读回/修改/连接/目录/过期确认/审计/断开。
- MCP 脚本只使用应用登录令牌，不需要获取 MCP 密钥；默认测试部署批准的
  `campus-tools` / `http://mcp-demo:8090`，可用非密钥变量 `APITEST_MCP_SERVER_NAME`、
  `APITEST_MCP_BASE_URL` 指定其他已批准目标。没有真实服务/配置时会明确失败，不以 mock 或 skip 代替。
- 为保证可重复运行，脚本复用固定批准名称并在最后断开，不删除教师已有登记；DELETE 脚本验证
  学生拒绝和不存在 404，成功软删除/名称保留由独立 MySQL HTTP 集成测试验证。
  不自动确认未知变更；若服务处于待确认状态，先人工核查完整内容后再运行闭环测试。
- 完整链要求非空 `ok/error/denied` 及其 SSE/回放/审计一致：先成功提问、参数校验故障提问，
  再断开并用原技能重问。每条审计必须匹配本轮 call_id、用户、会话、提问编号、轮次和摘要；
  空审计或网关 503 均失败，不再跳过流式断言。需为故障场景准备可控网关/注入条件。

可重复接口回归可使用显式测试网关 `apitest/fixtures/gateway.py`（Python 标准库），**不是真实模型**：

```bash
# 仅隔离验证环境；测试令牌由环境提供，不使用 MCP 令牌
python3 apitest/fixtures/gateway.py --host <验证私网地址> --port 18091 --dimension 1536
python3 -m unittest discover -s apitest/fixtures -v
python3 apitest/run_all.py --base-url <验证API地址> --gateway-evidence deterministic-fixture
```

先在私有环境设置非空 `APITEST_GATEWAY_TOKEN`，并仅将验证 API 的 CHAT_BASE_URL / EMBEDDING_BASE_URL
指向该地址的 `/v1`，相应 API_KEY 使用该测试令牌。使用 sudo Docker 时通过权限 0600 的临时 env 文件
注入，不扩大 sudo 环境权限，也不把令牌写入命令参数或版本库。测试网关提供两步调用、真实 SSE 帧及
固定维度测试向量；问题含「参数校验演练」时故意生成错误类型参数，验证宿主在出站前拦截并留审计。
结束后恢复验证 API 原配置并关闭测试进程；不要修改业务 `.env` 或把测试向量当成语义质量评估。
HTML 报告会标明网关证据类型；真实模型两轮闭环仍须独立验收，不能通过改报告标签替代。

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
