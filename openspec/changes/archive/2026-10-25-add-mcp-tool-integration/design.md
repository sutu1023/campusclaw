# Design

## Context

动机和课程口径见 `proposal.md`。本设计基于仓库实际实现，而非课程示范仓的内部结构：

- 主应用为 Go HTTP API + MySQL/Qdrant，前端为 Vue 3/Vite；`docker-compose.yml` 目前只有 db、qdrant、api、web，没有 MCP 服务。
- `internal/skill/parse.go` 只接受 `knowledge_search`；`route.go` 的候选 `Entry` 尚不携带工具。`internal/httpapi/stream.go` 从数据库候选转换时也未传递工具，生成过程直接调用一次 `CompleteMessagesStream`。
- `internal/chat/client.go` 的消息只有 role/content，没有工具定义、结构化调用或 `tool_call_id`；现有文本流消费者可以复用，不能通过放宽客户端消息角色来引入工具消息。
- `skills.tools_json` 是 `VARCHAR(255)`，不能安全容纳八个长 MCP 引用；`messages` 没有工具摘要字段且 role 只允许 user/assistant。`Store.Migrate` 目前仅逐条执行 `CREATE TABLE IF NOT EXISTS`，不能靠修改建表语句升级旧库。
- 路由统一使用 Bearer middleware 和 RequireTeacher；技能写入先解析后调用 Store，需增加本班数据库语义校验。前端 `ChatView.vue` 只读取 meta.skill/citations，回答结束后重新加载消息，因此仅做瞬时工具栏会在刷新时丢失。
- OpenAPI 由 swag 注解生成至 `docs/`；接口测试按 URL 路径组织在 `apitest/cases`。HTTP/Store 集成测试依赖显式测试 MySQL/Qdrant，缺少环境时会跳过。

## Goals / Non-Goals

**Goals:**

- 在现有鉴权、检索和 SSE 生命周期中增加一个独立、可测试的 MCP 编排边界。
- 让连接配置、可用工具快照、执行授权、累计额度和历史证据各有明确事实来源。
- 把 MCP 网络错误与工具协议错误限制为局部失败，最终答案沿用现有真流式路径。
- 保持旧技能、旧消息、历史窗口和 `/api/ask` 的行为，不要求清库迁移。

**Non-Goals:**

- 不增加另一个 Git 仓库或 Go module，不扩展消息表 role 为 tool，不把中间协议消息加入跨轮历史。
- 仅接入部署方批准的只读教学服务；不支持带副作用的任意第三方工具，因而本次不实现逐动作人工确认流程。
- 不实现 stdio、MCP resources/prompts、服务端主动通知、旧双通道 SSE、指标或分布式追踪。主应用按现有单 API 实例部署，不为多副本连接状态广播新增基础设施。

## Decisions

### 1. 同仓 Go MCP 服务，有限的 HTTP JSON-RPC 互操作配置

在 `mcp-server/` 新增 Go main、HTTP handler、工具实现、测试和 Dockerfile，复用根 go.mod，通过 `go build ./mcp-server` 构建。服务监听 `:8090`，在 Compose 内命名为 `mcp-demo`，无宿主端口映射；健康检查由可执行文件的健康探测模式完成，不依赖 distroless 镜像不存在的 curl。

采用单端点 HTTP POST + JSON 响应的无状态配置，协议版本固定为双方实现的 `2025-03-26`；initialize 协商 tools 能力，宿主发送 `notifications/initialized`，随后 tools/list。通知返回无 JSON-RPC 响应正文；不宣称实现所有可选的 Streamable HTTP 会话/服务端推送能力。有效方法错误采用 JSON-RPC 标准代码 -32700/-32600/-32601/-32602；认证先于解析，鉴权错误使用 HTTP 401。健康检查不认证。服务从环境读取非空 MCP_AUTH_TOKEN，令牌比较避免时序差异，错误不回显请求头。

两个工具均以 `{query: string}` 为输入，要求 1–500 个字符、无额外属性；`formula_hint` 提示公式适用条件，`unit_convert` 提示单位关系和方向，不计算练习的最终数值。工具本身不访问网络、不写文件。

**取舍：** 不引入完整 MCP SDK，避免课程只需的三个方法引入无关能力；协议互操作由真实进程测试保障，不把宿主 mock 当成服务验收。所有界限在 README 明示。

### 2. 环境凭据绑定，不接受浏览器提交密钥

采用部署批准表 `MCP_CREDENTIAL_BINDINGS`（非密钥 JSON），每项包含 `class_id`、`server_name`、规范化 `base_url`、`token_env`。示例映射为班级 1 的 campus-tools / http://mcp-demo:8090 / MCP_AUTH_TOKEN；真正令牌仅在本机 `.env` 的 MCP_AUTH_TOKEN 中填写，Compose 把同一个变量传给 api 和 mcp-demo。

教师只登记名称与地址；服务端从当前班级 + 名称 + 地址精确匹配部署批准项，数据库只保存绑定标识。配置中的 token_env 只能来自启动时验证的 MCP 凭据变量白名单，不能由 HTTP 请求任意查环境。响应仅含 has_token，不返回凭据原文或环境变量内容。每个新增服务器使用独立的凭据变量，不能跨批准目标复用；不允许借通用全局 token 配置去调用任意 URL。

Base URL 只接受批准的 origin，移除单个根路径尾斜线，拒绝 userinfo、query、fragment、非根 path；HTTP 客户端不跟随重定向。允许 Compose 私网 HTTP 是显式部署信任，不是把任意私网地址整体放开。连接/调用前均重新核对该绑定，避免修改地址把令牌外发。缺失 MCP 配置不阻止主应用启动，管理界面提示未配置；独立 MCP 服务自身仍拒绝无令牌启动。

**取舍：** 不采用教师在 SPA 输入明文令牌并写数据库的示范方案；环境引用更严格地满足“令牌不进前端、只在本机 .env”的要求，也免去可恢复密钥加密和密钥轮换表设计。运维须先批准目标，教师不能自行接入任意第三方地址。

### 3. MySQL 是连接快照和授权的事实来源

新增结构（命名可在实现中按现有 Store 风格细化）：

| 数据 | 关键内容与约束 |
| --- | --- |
| mcp_servers | class_id/name 唯一；不可改名；base_url、credential_ref、disconnected/connected/pending_approval、generation、approved_hash、last_error、deleted_at；保留 tombstone，避免旧技能引用被新服务器同名接管 |
| mcp_tools | server_id/name 唯一；工具完整 description、input_schema、snapshot_hash；仅保存当前可用快照 |
| mcp_discovery_snapshots | 按 server_id/hash 保存有界的完整发现定义历史；服务器记录分别指向上次批准 hash 和当前待确认 hash，历史不是可调用目录 |
| mcp_discovery_changes | class_id/server_id、前后 hash、变更工具名、时间、确认教师/时间；不存密钥 |
| mcp_tool_calls | class_id、server_id、server_name/ref 快照、user_id、conversation_id、question_message_id、call_id、round、status、safe_text、created_at、duration；按本班时间和会话/提问索引 |
| mcp_conversation_usage | conversation_id 唯一、reserved_count；用于跨提问、跨重启及并发预算 |

首次连接成功：先握手/发现并在内存中完整验证，再在短事务内整体替换工具并设置 connected。重复连接不追加清单；删除的工具不保留。已有工具 description 或 inputSchema 改变时，写变更记录，保存 pending 快照，清除可用工具并标 pending_approval；教师看完整差异后提交 hash 确认，事务内激活该快照。新增和删除工具在教师主动重连时更新并记录，新引用仍须经技能保存校验。没有实现自动远端变化探测，发现是教师重连触发的快照。

连接开始先以 generation 使旧许可失效；任一步失败都维持 disconnected、无可用目录。网络期间不持数据库事务。完成连接/确认时按 generation 比较更新，避免并发断开、地址修改或另一轮连接被迟到的结果覆盖；过期操作返回 409。断开清空可用和待确认快照，generation 递增，幂等。删除采用 tombstone，历史审计不级联删除。运行时新授权必须读取当前状态及 generation；已在断开前授权并发出的只读调用不撤回。

断开或重启只清空可用目录并取消待确认指针，不删除上次批准的定义历史；重连须与该历史比较，不能把断开后的变更当作“首次发现”绕过确认。历史快照不作为 tools 接口的可用工具返回。

API 重启时以短事务将遗留 connected/pending 状态失效并清空目录，但保留登记、批准历史、变更和用量，要求重新连接；这样不把跨进程的陈旧握手状态当成可用连接。无需为原来的随机登录 token 改动任何存储。

**取舍：** 不只使用进程 map 缓存，避免重启丢登记、审计和配额；不在数据库事务中进行远程请求。启动失效换取简单的安全语义，代价是重启后需教师重新连接。

### 4. 管理 API 与保存前校验

所有以下路由使用现有 auth middleware + RequireTeacher：

| 方法 | 路径 | 成功/主要失败 |
| --- | --- | --- |
| GET / POST | /api/mcp/servers | 列表 200 / 登记 201；非法或重名 400 |
| GET / PUT / DELETE | /api/mcp/servers/{id} | 200 / 200 / 204；不存在或他班 404 |
| POST | /api/mcp/servers/{id}/connect | 200，含 connected 或 pending_approval 状态；配置错误 400、远端失败 503、并发失效 409 |
| POST | /api/mcp/servers/{id}/disconnect | 204；无资源 404 |
| GET | /api/mcp/servers/{id}/tools | 200，tools 与可选 pending_tools/变更标识；无资源 404 |
| POST | /api/mcp/servers/{id}/approve | 请求 {snapshot_hash}，成功 200，过期 409 |
| GET | /api/mcp/audit | 200；page/page_size(默认20、最大100)、server_id/status；他班 server_id 404 |

公共 DTO 不复用含内部凭据引用的 Store 原始模型。所有错误采用项目的 `{error}` 风格并脱敏，401 指应用身份失败，远端 401 在 connect 映射为 503 连接失败，不触发浏览器登出。

技能解析只处理语法、去重与八项上限；新增可测试引用解析器。数据库验证放入技能 Create/Update 的写事务，锁定本班服务器状态与有效目录后再写技能，以确保检查失败不留下部分更新。更新先验证技能本身归属，其他班和不存在服务器统一 ErrNotFound；未连接/待确认/未发现映射可读 400。`route.Entry` 增加 Tools，HTTP 转换必须保留它，词法计分规则完全不变。

**取舍：** 不在纯解析器中发网络请求或注入数据库，不在保存时自动连接。工具名称绑定和运行期许可是两个独立防线。

### 5. 离线 Schema、体积和超时限制

新增 `internal/mcp` 协议客户端与定义验证边界，使用一个固定版本、离线运行的 JSON Schema 校验器（拟采用 santhosh-tekuri/jsonschema/v5）。发现阶段编译对象输入结构；仅接受本课所需类型、properties/required/additionalProperties、enum、长度及数值约束，拒绝远程 `$ref`、无法支持的结构和过深输入；不允许模型或服务借 Schema 触发宿主额外网络访问。

默认网络请求超时 5 秒；整份 MCP 响应最大 1 MiB，发现最多 64 个工具，单个 Schema 最大 64 KiB、说明最多 2000 字符；工具参数 JSON 最大 16 KiB、嵌套深度最多 16；有效工具结果进入模型前截断到 2000 字符，SSE/审计摘要最多 500 字符。错误响应不直接转发上游 body，避免远端回显 Authorization。超限在明确的本地错误分支停止，不从截断 JSON 中继续解析。

**取舍：** 不手写另一个宽松的类型检查器，不信任“参数由模型生成所以安全”；不支持的 Schema 在发现时拒绝而非等到学生提问才失败。

### 6. 两阶段模型编排，最终答案仍真流式

新增独立编排器（如 `internal/toolrun`），依赖候选读取/授权、MCP client、网关工具决策和审计接口。handler 只负责既有会话流程、SSE 和最终消息落库，不把整套调用循环塞进 stream.go。

流程：

1. 沿用身份、会话、固定智能体、技能路由、材料过滤、资料编号和历史窗口；问题先落库取得 question_message_id。
2. 输出首次 meta；解析命中技能 Tools。无效/失效引用立刻生成有来源的 denied 事件；可用引用形成最小工具候选。只有无有效候选时跳过模型工具阶段。
3. 为候选生成满足网关 64 字符命名限制的稳定别名（例如前缀 + server_id + 工具名短哈希），以映射表恢复完整 ref，并检测碰撞。发现的 description/inputSchema 原样用于工具声明；不传 URL/密钥。
4. 使用非流式工具决策请求，读取 assistant.tool_calls；若存在，按顺序处理，每次在授权与参数校验后预留配额/审计，执行真实 tools/call。每个请求有唯一 call_id，结果使用相同 tool_call_id；每轮的 assistant 调用列表与每条 tool 结果成对追加进本轮内存 messages。
5. 每轮处理完发送一次 meta，仅包含新增的工具摘要，保持相同 skill/citations。将工具错误也作为固定 tool 数据回传，允许模型在后续轮次选择其他有效工具或修正参数。
6. 模型不再请求工具、工具阶段失败或耗尽预算时，结束决策阶段。决策请求即使返回整段 content 也不转发或落库；明确关闭工具选择，使用累积工具数据重新调用已有真流式补全。工具选择已关闭，不能继续执行模型提出的动作。
7. 只把最终正文增量写入 delta/最终回答。维持无证据去编号、部分生成持久化与 done 语义；工具摘要随最终助手消息落库，刷新后从消息读取。

无 MCP 技能直接沿用一次流式请求，不增加延迟。工具阶段网关不支持 function calling 或结构错误时，向 meta 给出关联候选的阶段 error，退回工具阶段之前的可信正文消息，移除 tools/tool_choice 及中间工具协议消息，再尝试原有流式路径；不把工具结果偷换到 system/user 位置，也不制造远端 ok 审计。正常支持工具的最终生成只关闭工具选择、保留已成对的工具数据。整个阶段收到 HTTP 上下文取消就停止网络请求，不为断开的客户端继续生成。

**取舍：** 工具决策用非流式结构化响应，最终答案额外生成一次；这比在现有文本流解析器里缓存/拼接尚未完整的 tool-call 增量更容易验证，也避免为了工具进度把缓冲好的完整答案伪装成 SSE。代价是 MCP 问题增加一次最终请求，决策阶段的 content 不作为用户可见答案。

### 7. 配额与审计先于网络动作

服务端配置新增正数限制：MCP_MAX_ROUNDS=3、MCP_MAX_CALLS_PER_TURN=8、MCP_MAX_CALLS_PER_CONVERSATION=40、MCP_TURN_TIMEOUT=20s、MCP_REQUEST_TIMEOUT=5s。超大或非法配置启动时报可读错误；总工具阶段 deadline 同时覆盖模型决策与 MCP 请求，但为后续最终生成保留独立上下文预算。

每次模型提出的调用（包括参数错/被拒）计入本问尝试次数；同一批多个调用分别计数。决策响应最多接收 16 项，过大列表按有界阶段错误拒绝，不把未知长列表变成无限循环。会话累计额度只统计已预留的真实远端动作，成功/失败均消费，不因网络失败返还；固定的配置期 stale 引用拒绝不消费远端额度。

调用前用短事务同时锁定会话用量、核对服务器 generation、插入审计预记录并增加计数，提交后才发网络；预记录可暂以 error/interrupted 表示尚未完成，收到结果后更新为最终 ok/error。这样进程崩溃仍留下可追溯记录；事务失败不出站。最终审计更新失败保留 interrupted 记录，本次应答将该调用标为 error，而非静默报告成功。不同提问争用同一用量行，不能超额。审计失败只使工具阶段拒绝，不使问答返回 500。

对于本班可归属服务器，参数错、未连接和配额拒绝也记录审计；缺失、已删除、跨班或模型未知别名只写本次应答 denied，不通过探测他班服务器制造日志。删除旧服务器不删除已完成记录。分页按时间/id 倒序，所有条件从会话 class_id 起始。

**取舍：** 不仅在 handler 局部变量计数，不用 COUNT 历史成功记录计算配额；前者重启可绕过，后者并发可超用且遗漏失败动作。成功结果摘要是安全展示数据，不是外部返回的原始存档。

### 8. 提示数据边界与展示协议

扩展固定系统边界句，声明工具说明/结果也是不可信数据。网关工具定义不是权限来源；每次执行复核候选和当前状态。发现定义和工具文本在进入模型/浏览器前均检查是否包含当前 MCP 或模型网关的真实凭据值，命中直接拒用，不输出匹配片段。工具文本还须统一检查空结果、非文本、isError、长度以及已知注入句式（例如忽略既有指令、索取密钥、外发凭据）；命中整条丢弃，以固定失败 tool 消息替代，并记录安全错误，不把可疑原文写 SSE 或审计。

客户端和数据库历史继续只接受 user/assistant 正文。单独的宿主消息构造器才能写 assistant.tool_calls / tool_call_id，不能简单把 SanitizeMessages 改成接受任意 tool。`knowledge_search` 继续执行原来的智能体材料检索，不开放由模型任意指定 class_id/material_id 的新入口。

SSE 仍使用 meta/delta/done/error 四种事件。首次 meta 可无 tool_calls；后续 meta 每条只携带新增记录：`{call_id, ref, round, status, text}`，未知/阶段错误用安全 ref 和固定原因，不含参数原文或地址。ChatView 合并而不是覆盖，call_id 去重；不需要新 EventSource 或 WebSocket。

**取舍：** 不以正则过滤宣称能完全防注入；真正动作边界仍是批准的只读服务、逐工具授权、Schema 和配额。工具完整说明在管理界面可见，变更须重新确认；最终回答外发意图扫描留给第 8 课。

### 9. 可验证的数据库升级与旧数据兼容

新建表继续使用幂等 DDL；另为旧库增加按 information_schema 判断的列升级步骤：skills.tools_json 扩为 TEXT，messages 增加可空的 tool_calls_json MEDIUMTEXT。读取 NULL 映射空集合，不回填伪造事件；消息 role ENUM 和原来的 6 条/500 字历史不变。新调用摘要不作为下一轮可执行 tool 消息重放。

旧库、新库、重复迁移分别测试，更新 HTTP 集成测试的 DROP TABLE 依赖顺序，避免新增外键使清理失败。DTO 与 Swagger、前端 ChatMessage 同步增加可选/兼容的工具摘要。

**取舍：** 不靠 CREATE TABLE IF NOT EXISTS 修改已有列，不重置 MySQL 卷；保留字段扩容允许应用回滚，不在回滚时把长文本强缩回 255 字节。

## Risks / Trade-offs

- [实际 course-chat 网关可能不支持工具协议] → 使用现有配置，不擅自换模型；用可控网关桩证明协议、多轮和上限，用真实环境做兼容验收；工具协议失败显式降级，不能算作完整工具闭环验收通过。
- [说明投毒和语义绕过仍可能影响模型] → 全文可见、快照变更确认、结果数据位和已知句式检查；限定只读自建服务、逐调用授权和硬配额，不宣称彻底消除注入。
- [批准 origin 仍依赖部署 DNS 信任] → 仅 Compose/运维批准的 origin、无重定向、凭据与精确目标绑定；任意第三方、DNS 重绑定通用防护不在本次开放范围。
- [断开与在途调用交错] → generation/CAS 阻断迟到连接和新许可；明确只读在途请求不撤回，验证断开后的下一次提问零远端请求。
- [审计或配额数据库不可用] → 动作 fail-closed，保留原问答可继续的能力，不用“没有日志”冒充执行成功。
- [多次 meta 对旧前端的兼容] → 增量字段和稳定 skill/citations 保持旧客户端可忽略；新前端按 call_id 合并，增加分帧、重复事件和刷新回放测试。
- [迁移和当前集成测试有外部依赖] → 使用独立测试库/Qdrant，不对生产卷运行破坏性测试；记录通过与跳过，不能把本地单测通过当成 Compose 验收证据。

## Migration Plan

1. 在测试副本运行新增表、旧列扩容、工具摘要列迁移，重复执行确认幂等；备份现有 MySQL 数据，不删除卷。
2. 在本机 `.env` 设置独立 MCP_AUTH_TOKEN 和不含密钥的绑定配置；`.env.example` 仅空值/非密钥示例。检查两个构建上下文均排除 .env，Dockerfile 不使用 secret ARG/ENV，web 不接收任何 MCP 令牌。
3. 构建并启动同仓 mcp-demo 和升级 API/web；API 不以 MCP 网络可用为启动前提。所有既存登记要求重新连接，历史数据保持可读。
4. 运行服务级鉴权/JSON-RPC 测试、带明确测试环境的 Go 集成测试、前端测试/类型检查/构建、OpenAPI 再生成检查和 `python3 apitest/run_all.py`；HTML 报告继续忽略。
5. 完成真实 Compose 闭环：健康/发现/单工具调用 → 教师连接 → 技能/智能体绑定 → 学生提问/两轮调用 → meta 与审计 → 断开再问 denied。补充跨班隔离、密钥不回传、断开拒绝和工具故障降级的自动化验证，以及镜像/静态资源检查证据至 VERIFICATION.md，截图/日志必须脱敏；按用户决定，不另设同伴交叉验收流程。
6. 回滚时先断开所有 MCP 服务、停止 mcp-demo，再回滚 API/web；保留新增表/扩容列和审计，不破坏性降级数据库。升级期间创建的 MCP 技能须先改回内置工具或停用，避免旧版本拒绝其工具语法；原有材料、会话和旧技能不需恢复。
