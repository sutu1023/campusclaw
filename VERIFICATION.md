# 验收报告：add-auth-rbac-class-knowledge

对应 OpenSpec change `add-auth-rbac-class-knowledge` 的 tasks 7.2。以下三条为
`specs/auth-upload/spec.md` 的关键 Scenario，均在 Docker Compose 启动的实例上手工复验。

运行环境：`docker compose up --build -d`，应用监听 `http://localhost:8080`。
预置账号密码：`teacher_a` / `student_a1` / `student_b1`，口令 `CampusClaw#2026`。

| # | Spec Scenario | 操作 | 期望 | 实测 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 跨班按 ID 访问材料被拒绝 | `teacher_a` 会话请求 `GET /api/materials/2`（B 班材料） | `404`，响应无 B 班标题/正文/路径 | `HTTP 404`，body `{"error":"not found"}`，无「B 班」内容 | 通过 |
| 2 | 学生上传被拒绝（403） | `student_a1` 会话 `POST /api/materials/upload` 提交 `.md` | `403`，`materials`/`knowledge_entries` 与上传目录无新增 | `HTTP 403`，body `{"error":"forbidden"}`，行数与文件数不变 | 通过 |
| 3 | 上传后知识库与列表可查 | `teacher_a` 上传 `验收-第三场景材料`，随后 `student_a1` 请求 `GET /api/materials` | `201` 且学生本班列表可见该标题 | `HTTP 201`（material_id=3）；`student_a1` 列表含「验收-第三场景材料」 | 通过 |

补充：跨班统一返回 `404`（实现择一，已在 README「权限与班级隔离约定」中固定）。
`openspec validate add-auth-rbac-class-knowledge --strict` 返回 valid（exit 0）。

## MCP 验收：add-mcp-tool-integration（2026-10-21–22）

以下仅对应任务 2.3–2.4 的服务/部署边界，**不代表整个 change 或模型对话闭环完成**。
使用独立 Compose 项目 `campusclaw-mcp-verify`、独立 MySQL/Qdrant/上传卷和临时生成的环境凭据；
本机原 `.env` 与既有数据卷未修改。Docker 命令通过 `sudo -n docker` 执行，未更改 socket 权限或用户组。

| 检查 | 脱敏证据 | 结果 |
| --- | --- | --- |
| Compose 配置与构建 | `docker compose config --quiet`；API/MCP 镜像均构建成功 | 通过 |
| API 独立启动 | 先仅启动 db/qdrant/api，MCP 未启动时 `/health` 已返回 `{"status":"ok"}` | 通过 |
| 内网与凭据注入 | MCP 无宿主端口；API 无 MCP depends_on；两个服务运行时令牌相同，仅比较不输出 | 通过 |
| 真实 MCP 健康 | Compose healthy；容器执行 `/mcp-server --healthcheck` 返回 0 | 通过 |
| API 容器真实协议互通 | `/mcpcheck` 完成 initialize/initialized/list；协议 `2025-03-26`，发现 2 项 | 通过 |
| 真实工具调用 | `formula_hint: ok, 80 字符`；`unit_convert: ok, 100 字符` | 通过 |
| 错误令牌 | 仅覆盖检查进程的环境后 `/mcpcheck -expect-unauthorized` 返回 `authentication=unauthorized` | 通过 |
| 构建上下文隔离 | 实际构建 COPY 根上下文再导出检查：源码存在，所有 `.env`/`.env.*` 与已配置密钥均不存在 | 通过 |
| 镜像层隔离 | 导出 API/MCP 镜像，检查配置与 17 个层归档：无环境文件、无已配置密钥 | 通过 |
| 单测 | `go test -race ./cmd/mcpcheck ./mcp-server ./internal/mcp ./internal/config` | 通过 |

健康/发现/调用/错误令牌步骤按 README 复跑通过。CLI 仅打印工具名、状态和字符数，
未打印真实令牌、远端原文或原始网络错误。本节不涵盖数据库升级、教师管理、技能绑定、模型提参、
多轮编排、SSE/回放、前端及跨班闭环；后续任务须分别补齐，不能以本节服务级结果代替。

### 持久化与兼容迁移（任务 3.1–3.5）

在上述独立 MySQL 实例中另建可丢弃的 `campusclaw_mcp_store_test` 数据库，
不复用运行中 API 的业务库。执行 `go test -race -p 1 -json ./internal/store ./internal/httpapi`：
**56 个测试/子测试通过，0 失败，0 跳过**；Qdrant 亦使用独立测试实例。

- 六张 MCP 表、班级范围查询、同班唯一名和 tombstone 名称保留均通过。
- 新库、模拟第 5 课原列形状、连续三次迁移通过；长引用无截断，旧消息 NULL 读为 `[]`，
  原材料/技能/会话与 role ENUM 保留；原 6 条/500 字窗口及旧 HTTP/SSE 测试通过。
- 重连整体替换、工具移除、JSON 排序/空白不误判变化、描述/Schema 待确认、
  迟到连接/过期确认拦截，以及断开/启动失效后保留批准比较历史均通过。
- 两个并发提问争用最后一个额度：只授予一个许可；新连接池与启动失效不重置累计用量。
- 通过测试库临时 CHECK 约束模拟审计预写和终结失败：预写失败零许可/零消费，
  终结失败保留 `error/interrupted` 证据；没有开启全局不安全选项或要求 SUPER 权限。
- 成功/错误/拒绝审计倒序可查；跨班查询/筛选不泄漏；删除服务器或会话后历史来源仍在。

迁移与应用回滚步骤见 `docs/MIGRATION.md`。本节的早期测试只验证兼容升级与原功能回归，
不替代整套应用镜像回滚；完整演练证据见后文任务 10.3。工具编排、管理 UI 与真实模型验收亦分别记录。

### 教师管理、发现与审计 API（任务 4.1–4.5）

独立测试环境的 MCP HTTP 集成测试通过，包括所有管理路由 401/403、伪造班级忽略、
跨班/不存在一致 404、同名/软删除名称保护、无凭据回传、坏令牌/超时/版本/非法清单失败、
完整快照替换、描述/Schema 变更比较、并发确认只有一次成功，以及迟到连接不能覆盖断开。
工具定义及嵌套 Schema 的真实凭据回显（含 JSON Unicode 转义）整条拒绝，不落入可用目录。

- 使用 Compose 中真实教学服务验证管理 connect/tools 成功。
- 重新构建并部署 API 后，按教师登录 → 登记 → 连接 → 读取两项完整工具执行；
  实际重启 API，确认登记与 approved_hash 保留、tools 清空且 state=disconnected。
- API 容器内服务级 `/mcpcheck` 仍使用已批准内网地址，不需把 MCP 密钥交给测试脚本。
- 运行登录及全部 MCP 管理路径脚本：**39/39 断言通过**；HTML 写入被忽略的
  `apitest/report/mcp-management.html`。脚本不自动确认未知变更，也不删除固定批准登记，
  成功软删除场景由隔离 HTTP 集成测试验证，保证脚本可重复运行。
- 审计 HTTP 测试验证 ok/error/denied、用户/会话/提问/轮次回溯、倒序分页和跨班过滤；
  当前部署脚本只验证管理与筛选，不以空审计目录冒充真实模型工具调用成功。

### 技能引用与独立工具编排（任务 5.1–6.8）

- 纯解析器验证分隔符、大小写/命名、去重、八项上限；原词法路由计分与绑定范围测试通过。
- 独立 MySQL 的技能 HTTP/Store 测试验证跨班/缺失一致 404、不可用/未知工具 400、
  失败更新原文/字段不变、仅内置项兼容；八个最大长度引用文件可完整写入并回读。
- 实际重建部署 API，管理 + 技能脚本 **52/52 断言通过**，含连接后保存、完整回读、
  断开后拒绝保存且原文保留；报告 `apitest/report/mcp-skills.html` 未跟踪。
- `internal/toolrun` 和 `internal/chat` 的确定性网关桩验证两轮依赖调用、批量逐项结果、
  宿主稳定标识、同源 Schema、候选外零出站、错误后可继续决策及三类安全摘要。
- Compose 中真实 MCP + 独立 MySQL 的 Engine 集成测试验证成功/参数错误/断开拒绝的
  用户/会话/提问/轮次审计，以及实际 CHECK 约束导致审计预写失败时零额度消费。
- 并发 Engine 争用最后一个远端额度只产生一个成功；新数据库连接池执行启动失效并重连后
  仍拒绝新调用，累计值保持 1。没有用进程 map 或成功记录数冒充持久化配额。
- 配额/取消/阶段 deadline、17 项决策整体拒绝、超时/401/非文本/超大响应、已知注入与
  MCP/网关密钥回显（含 Unicode 转义和换行）测试通过。可疑文本整条丢弃，不入成功摘要。
- 流式网关桩在第一段后阻塞：调用方已收到第一段，释放后才收到第二段；最终请求明确
  `tool_choice=none`，工具结果仍在 tool 位。工具最终响应若只是 JSON 或继续提出动作会被拒绝，
  不拆分缓存全文假流；协议回退前后成功审计数只等于真实调用数。

阶段全量回归：在显式独立 MySQL/Qdrant/真实 MCP 环境执行 `go test -race -p 1 -timeout 120s -json ./...`，
**254 个测试/子测试通过，0 失败、0 跳过**；严格 OpenSpec 校验及 `git diff --check` 通过。
上述第 6 组结果是独立编排器/协议验证；会话接入见下节，不能替代真实模型网关两轮验收。

### SSE 生命周期与消息回放（任务 7.1–7.4）

已把独立编排器接入 `sendMessage`。确定性模型网关桩 + **Compose 中真实 MCP 服务** +
独立 MySQL/Qdrant 验证通过：

- 首次 meta 无调用字段，随后第 1/2 轮新增工具 meta，最后才出现正文 delta；三次 meta 的
  skill/citations 相同、外层 status=ok，两轮 call_id 可区分且与审计/工具消息对应。
- delta 拼接等于 done 所指的助手正文，摘要按原顺序完整回读；后续提问只重新携带正文历史，
  每问只多两条持久化消息，没有保存或重放中间协议，NULL/无工具摘要都读取为 `[]`。
- 固定系统边界末尾覆盖材料/技能/工具说明和结果；伪造客户端 system/tool/call 字段、
  存储摘要中的伪造协议都不进入可信链，解题引导硬约束及材料范围回归通过。
- 实际断开后原技能下一问两个引用均第 0 轮 denied，不再次请求模型决策或远端动作；
  主回答继续、原文保留、拒绝可回放，跨班会话和不存在均 404。
- 真实 MCP 错误认证、请求超时、实际 CHECK 约束审计预写失败、网关拒绝工具协议时仍生成正文；
  错误均脱敏，失败不冒充成功审计，无依据去编号，应用身份不因远端 401 失效。
- 最终生成零正文失败使用 SSE error，不保存助手；部分正文后失败沿用保存/done，并保留摘要。

再次执行显式独立环境下 `go test -race -p 1 -timeout 120s -json ./...`：
**267 个测试/子测试通过，0 失败、0 跳过**；严格 OpenSpec 校验及差异空白检查通过。
本节不代表真实模型网关已验收；UI 验证见下节，Swagger 浏览器试调、整套接口及回滚仍待完成。

### 教师与学生界面（任务 8.1–8.4）

- 新增教师专用 `/mcp` 导航与异步角色守卫：学生无入口、直达被拒；应用 Bearer 注入、401 清除
  token/角色与跳转、远端 503 不误登出均有测试。MCP 页面无凭据输入或本地 MCP 密钥存储。
- 组件测试覆盖登记/地址修改/删除、has_token、连接、完整说明/Schema、前后快照与当前 hash 确认、
  过期 409/连接失败后不残留可用目录、断开、分页/状态与服务器筛选，以及安全审计来源。
- 技能页只给教师展示已连接工具参考；后端保存错误原文可见、编辑原文不丢失，断开/待确认移出参考；
  学生不查询教师目录，也没有写入口。
- 聊天测试覆盖 CRLF/UTF-8 跨字节分帧、多轮元数据先于正文、重复 call_id 合并、三种状态、刷新回放、
  无工具隐藏栏、HTML 以文本转义、错误后保留正文/状态，以及离开页面 abort fetch 不继续刷新。
- `npm --prefix web run typecheck`、`test`、`build` 均通过：**9 个测试文件、28 个测试通过**。
  `npm --prefix web run lint -- --format json`：**0 错误、137 风格警告**（基线 213 个警告；未禁用规则）。
  构建输出仍在忽略的 `web/dist/`；没有把依赖、报告或构建产物加入索引。

这是组件/前端构建证据，不冒充浏览器 Swagger 试调或真实模型两轮闭环验收。

### OpenAPI 生成、路由一致性与真实浏览器（任务 9.1–9.2）

- `make docs` 已从注解生成 docs.go / swagger.json / swagger.yaml；连续生成的 SHA-256 一致。
  生成器提示根目录无 Go 文件，但正常完成；生成物解析、全部 MCP 方法及对应错误码测试通过。
- 公共服务器 DTO 不含 token/credential_ref/token_env/generation；Schema 是 object 而非字节数组，
  消息摘要使用三种状态枚举；Bearer 描述明确为应用登录令牌，不要求 MCP 密钥。
- 检查生成文档未包含本机或独立测试环境的配置密钥。SSE 多次 meta、call_id、状态和部分生成说明已同步。
- 在独立数据库环境运行文档/路由测试：三个测试通过。全部 10 个文档 MCP 方法实际命中
  401 / 教师角色 403 中间件，原健康、登录、Swagger 页面和业务路由未被文档挂载覆盖。

此前官方 Playwright Docker 镜像下载超时。经用户同意，改从官方 CDN 下载 106.1 MiB 的
Chromium headless shell，版本 **141.0.7390.37 / build 1194**，Playwright **1.56.1**。
Ubuntu 26.04 ARM64 不在该版 Playwright 官方支持映射内，因此仅对下载进程指定
`PLAYWRIGHT_HOST_PLATFORM_OVERRIDE=ubuntu24.04-arm64`；该版 22.04/24.04 ARM64 映射同一个包。
从主机已配置的 Ubuntu 源下载八个小型运行库到临时目录解包（含两个传递依赖），仅为测试进程设置
LD_LIBRARY_PATH；没有安装系统包、修改系统版本或额外提权。ldd 无缺失库，真实浏览器启动及页面渲染通过。
此处是实际运行验证，不宣称 Playwright 官方支持当前宿主版本。

重新构建并部署独立验证 API 镜像 `campusclaw-mcp-verify-api:working`，不覆盖原应用镜像标签。
运行 `apitest/swagger_browser.cjs`，**14 次真实 Swagger UI Execute 请求通过**：

- 无应用令牌列表 401；Swagger 登录成功并注入应用 Bearer，后续请求核对实际请求头。
- 复用固定批准登记、重复登记 400；connect 200 / connected，tools 200 返回两个完整说明与对象 Schema。
- approve 提交明确过期 hash，实际处理器返回 409；未伪造工具变更或自动确认未知内容。
  成功批准和并发确认分支另由第 4 组集成测试覆盖，不把本次 409 说成批准成功。
- 新建 MCP 技能 201，GET 读回完整原文和引用；审计与 /api/me 均 200。
- 断开 204，再保存 MCP 技能 400，回读证明已有原文未改变。
- 浏览器全部请求同源 API；未提供 MCP 密钥，也没有浏览器直接请求 MCP 服务。

报告 `apitest/report/swagger-browser.json` 仅保存版本、分支说明和方法/路径/状态码，已确认被 Git 忽略；
未保存含应用令牌的响应、截图、HAR 或 trace。本次是浏览器文档/管理试调，不代表非空三态审计或真实模型闭环验收。

### 完整可重复接口链（任务 9.3）

独立 Compose API + **真实 mcp-demo/MySQL/Qdrant**，对话及嵌入使用明确标识的
`apitest/fixtures/gateway.py` 确定性测试网关，不是真实模型。测试网关仅绑定验证私网地址，
要求独立测试令牌、不读取 MCP 凭据、不转发请求、不记录头或正文；固定测试向量不用于语义质量评价。
四个 fixture 单测通过（ResourceWarning 作为错误），覆盖鉴权、向量结构、两步/参数错误和 SSE。

- `python3 apitest/run_all.py ... --gateway-evidence deterministic-fixture` 连续两次
  **119/119 断言通过**。覆盖管理 → 技能保存 → 智能体绑定 → 会话 → 成功/错误提问 → 断开重问 → 审计。
- 参数错误演练故意产生 `query` 数字类型，宿主 Schema 拦截后以 error 记录并继续正文；
  不把本地校验失败声称为远端工具执行失败。成功路径实际调用两个教学工具。
- 三类 SSE 均有非空工具记录、正文 delta 和 done；meta 在正文前，字段稳定，
  done 正文与增量拼接相等，完整有序摘要可回读，每问仅落 user/assistant 对。
- 空审计/空状态筛选不再通过；每条当前提问审计必须匹配 call_id、用户、会话、提问、引用、轮次及摘要。
  最近一次脱敏记录：会话 3 / 提问 10 / 第 1、2 轮 ok；提问 12 / 第 1 轮 error；
  提问 14 / 两条第 0 轮 denied。断开后的原技能仍继续正文，不隐式重连。
- 初次配置注入因 sudo 清除环境而失败；改用 0600 私有临时 env 文件注入后两次通过，未使用 sudo -E。
  每次结束均恢复验证 API 原网关配置、关闭 fixture 并删除临时测试令牌文件；原业务 `.env` 未改动。
- HTML `apitest/report/mcp-full-chain.html` 明确标注 deterministic-fixture，已确认被 Git 忽略；
  Python 缓存、依赖和报告不跟踪。503 不算流式通过，也不跳过断言。

以上是确定性接口闭环证据，不替代任务 10.2 的真实模型参数生成/两轮验收。

### 最新全量回归（任务 10.1）

在显式加载独立 TEST_DB_DSN / TEST_QDRANT_URL / TEST_MCP_URL / TEST_MCP_TOKEN 的环境执行
`go test -race -p 1 -timeout 120s -json ./...`：**269 个测试/子测试通过，0 失败、0 跳过**。
Store/HTTP 共享可丢弃测试库，按 `-p 1` 串行；未把业务库作为清理对象。
包括原认证/班级隔离、材料上传/检索、`/api/ask`、智能体/技能/会话、历史窗口与流式失败契约，
以及新增 MCP 客户端/管理/编排/审计/SSE/文档测试。完整接口回归见上一节的两次 119/119，
其中模型/嵌入使用明确标识的 fixture，MCP 和持久化为真实服务。

前端重新执行 typecheck / test / build 均通过，**9 文件、28 测试通过**；lint 为
**0 错误、137 风格警告**，未隐藏警告或禁用规则。Python fixture 四个单测亦通过，
ResourceWarning 作为错误检查；HTTPError 响应现以 context manager 关闭。

本节不包含真实模型两轮闭环或完整应用镜像回滚；对应证据须分别记录，不以全量回归结果替代。

### 真实配置网关两轮闭环（任务 10.2）

沿用本机原 `.env` 的有效网关配置及模型 **course-chat**（按既有规则复用嵌入网关的地址/密钥），
没有更换模型。初步协议探针使用真实发现的两个工具声明，返回 HTTP 200 和带 query 字符串的结构化调用。
随后独立 Compose API / **真实 mcp-demo / MySQL** 完成发现 → 技能 → 智能体 → 学生会话闭环。

原嵌入配置为 2048 维，而验证库为 1536 维；本次新建**不绑定材料**的教学智能体，只临时配置真实对话网关，
未修改嵌入配置、重建向量库或使用 fixture 对话。材料检索不在本次操作路径内，其回归证据见 10.1。
服务端临时透传代理只转发到原配置网关，用于记录结构元数据；不修改模型请求/响应、不合成决策或正文，
SSE 按上游每行立即转发，未等待全文缓冲。凭据仅在服务端内存/0600 临时文件，未记录头或参数原文。

| 步骤 | 实测脱敏证据 |
| --- | --- |
| 第 1 次模型决策 | stream=false / tool_choice=auto，HTTP 200；一个公式工具调用，query 为 33 字符字符串，Schema 有效、不同于原问题 |
| 第 1 轮执行 | `q16-c1`，`formula_hint@campus-tools`，round=1 / ok |
| 第 2 次模型决策 | 请求确实含关联 `tool_call_id=q16-c1`；HTTP 200；一个单位工具调用，query 为 57 字符字符串，Schema 有效、不同于原问题 |
| 第 2 轮执行 | `q16-c2`，`unit_convert@campus-tools`，round=2 / ok |
| 第 3 次模型决策 | 请求含两条关联 tool 结果，HTTP 200，不再提出调用 |
| 最终正文请求 | stream=true / tool_choice=none，上游 HTTP 200 / text/event-stream，85 个 data 帧（含协议增量），非缓存 JSON 拆分 |
| 回放与审计 | user=2 / conversation=4 / question=16；两条审计与本轮 call_id、ref、round、status、摘要完全一致；仅两条 user/assistant 消息，done 指向已保存助手 |

单调时钟记录：首次 meta **9ms**；第 1 轮工具 meta **2297ms**；第 2 轮工具 meta **4835ms**；
首个正文 delta **8360ms**；done **8494ms**。所有工具元数据早于正文；最终非空正文 50 字符，
增量拼接等于落库内容，完整有序工具摘要可刷新回读。工具阶段未改变默认 20 秒限制。
模型请求不含 MCP origin 或配置密钥原文；参数仅记录形状/长度/校验布尔值，不存 query 内容。

脱敏完整记录复制至忽略的 `apitest/report/real-gateway-acceptance.json`。
结束后断开验证 MCP 登记、恢复原验证 API 网关配置、关闭透传代理并删除临时凭据文件；原业务 `.env` 未改动。
这是实际已配置网关的验收，不是第 9.3 节的确定性 fixture；不代表应用镜像回滚已完成。

### 验收范围调整

按用户决定，本变更不安排同伴交叉验收；该流程已从任务清单移除，不计作验收通过。
跨班隔离、凭据不回传、断开拒绝和工具故障降级的功能要求与自动化验证仍保留，
证据见第 4 组、第 7 组、9.3 和 10.1，不删除已有测试或结果。

回滚演练重编号为任务 **10.3** 后已按下述步骤完成。当前 **45/45 项完成**；
移除的同伴流程没有计入通过数，也没有替代任何功能验收。

### 10.3 旧库升级、重复迁移与完整应用回滚（2026-10-22，完成）

**环境和证据口径**

- 从 `git archive 0d44216` 导出第 5 课基线到仓库外；不切换工作树，不覆盖原应用镜像标签。
  旧版为 `campusclaw-mcp-rollback-{api,web}:lesson5-0d44216`，
  当前版为 `campusclaw-mcp-rollback-{api,web}:current`；容器实际 image ID 与指定镜像逐项比对通过。
- 官方 `distroless:nonroot` 拉取曾连续两次 TLS 握手超时。经用户明确同意，MCP 使用此前已通过
  服务测试及真实闭环验收的本地镜像，别名 `campusclaw-mcp-rollback-mcp:verified-cache`，
  image ID 前缀 `d1ffae29c349`；不声称本轮重新构建 MCP 成功。未关闭 TLS、改镜像来源或修改 Docker 全局设置。
- 首轮隔离项目在升级和原数据行检查通过后，因浏览器登录撤销同账号旧会话、脚本继续使用旧令牌而遇到 401。
  这是验证脚本的会话管理问题，不计作通过；其备份与数据卷保留。修正脚本为浏览器检查后重新登录，
  在全新隔离项目 `campusclaw-mcp-rollback-r2` 完整重跑；没有修改应用单会话鉴权规则。
- 使用真实 MySQL/Qdrant、API、Nginx web 和教学 MCP 容器。聊天/嵌入网关为显式标注的确定性 fixture，
  仅证明升级/回滚兼容性，不冒充第 10.2 节已独立完成的真实网关验收。

**实际演练结果：68/68 主检查通过，0 失败**

| 阶段 | 实测 | 结果 |
| --- | --- | --- |
| 旧版建库和写入 | 真实旧 API 生成 `skills.tools_json VARCHAR(255)`，没有消息摘要列或 MCP 表；上传材料并完成嵌入，保存内置技能/智能体/会话及一对 user/assistant 消息 | 通过 |
| 旧数据备份 | 升级前、应用回滚前、应用回滚后各保存完整 `mysqldump --single-transaction --no-tablespaces` 和上传 tar；均为仓库外 0600 文件，未用于覆盖现库 | 通过 |
| 升级 | 创建全部六张 MCP 表，工具列为 TEXT、摘要列为可空 MEDIUMTEXT，role 仍仅 user/assistant；旧消息数据库值仍为 NULL、接口读取为 `[]` | 通过 |
| 原数据保留 | 11 张既有业务表的原行摘要在升级、重复迁移、回滚、再升级后均保留；逐项回读旧材料正文、完整技能原文/工具、智能体绑定、会话和消息/引用 | 通过 |
| 新版本真实工具调用 | 发现两项真实工具；fixture 提出参数，实际执行第 1/2 轮调用，均为 ok；meta 早于正文，回放匹配；审计 2 条，累计用量 2 | 通过 |
| 重复迁移 | 同库重启当前 API，列定义不变；旧数据、两条审计、累计用量、批准 hash 和发现历史不变；登记变为 disconnected、可调用目录清空 | 通过 |
| 回滚前置操作 | 仍在新 API 时断开所有登记，把升级期技能的两项 MCP 引用在解析字段及完整原文中替换为 `knowledge_search`，保留原文件备份，再停止 MCP 容器 | 通过 |
| API/web 应用回滚 | 切换到真正的 `0d44216` API/web 镜像；旧数据和升级期数据均可读，转换后的技能可保存，原升级期会话还能追加一对普通问答消息；MCP 保持停止 | 通过 |
| 再升级 | 恢复当前 API/web；同一会话的旧工具摘要和回滚期间新增消息同时可读，审计仍为 2 条、累计用量仍为 2、批准历史及扩展列均保留；不隐式重连 | 通过 |

原业务行等值比较不包含测试登录有意轮换的 sessions，以及既有 seed 每次启动重新生成的演示账号 bcrypt 盐；
账号 ID/角色/班级仍比较，登录会话在升级、重复启动及应用切换中的保留另行验证。
没有执行 DROP、缩列、删除数据卷或用备份覆盖升级后的数据；唯一预期的技能变化是回滚前的显式工具引用替换。

**真实浏览器验证**：复用第 9.2 节的 Chromium 141 / Playwright 1.56.1 运行环境，在旧版、升级后、
回滚后、再升级后四个实际 web 容器上完成 **26/26 页面断言**：教师登录/旧材料/旧技能、
随版本有无 MCP 导航、学生旧会话回放、升级期会话深链刷新及按版本兼容展示工具摘要；无 JavaScript 错误。
不是 jsdom 或仅 HTTP 200 验证，也没有保存 Bearer cURL、HAR、trace 或含密钥截图。

**最终制品安全检查：31/31 通过**

- 从原本机环境、既有验证环境及两轮演练环境读取实际配置值，仅在内存中比较；去重后 10 个密钥值，
  包括 MCP 令牌、网关 API key 与应用会话 secret，同时检查原文和常见 base64/URL/JSON Unicode 转义形式。
- Git 当前 276 个已跟踪/未忽略文件、暂存/未暂存差异，以及全部历史的 184 个 blob 均无匹配；
  私密环境文件、依赖、前端构建产物、报告、HAR/trace 不在跟踪清单内，`.env.example` 仅作为安全示例保留。
- 导出并检查**实际参与演练的 5 个运行镜像、64 个层归档和全部镜像配置**：无实际密钥，
  无私密 `.env` 文件；旧/新两个 Nginx 镜像的静态文件也在扫描范围内。
- 由 Docker 按实际 `.dockerignore` 构建 COPY 探针，检查当前根上下文（API/web/MCP 共用）及
  `0d44216` 导出上下文；均无私密环境文件和实际密钥，基线中非私密的 `.env.example` 允许存在。
- 本机 `web/dist` 的 3 个构建文件、7 份被忽略的接口/验收报告亦无实际配置密钥；
  新的脱敏报告为 `apitest/report/migration-rollback.json`，经 `git check-ignore` 和索引检查确认未跟踪。

两个演练项目的容器和临时 fixture 均已停止，保留独立 MySQL/Qdrant/上传卷和私密备份；
原 `.env` 内容摘要不变，既有 `campusclaw-mcp-verify` 的容器 ID、镜像及卷挂载不变。
本轮只完成验收与记录，未提交、推送或归档。

### 归档及提交前复验（2026-10-25）

- 已按用户选择同步全部 8 项能力（18 项新增要求、7 项修改要求），逐条核对且重复同步为无变化；
  保留未涉及的要求、场景与既有 Purpose。变更的 45/45 项任务全部完成，完整归档至
  `openspec/changes/archive/2026-10-25-add-mcp-tool-integration/`，包括 `.openspec.yaml`。
- 提交前重新加载独立测试库配置执行 `go test -race -count=1 -p 1 -timeout 120s -json ./...`：
  **269 个测试/子测试通过，0 失败、0 跳过**；没有对正常业务库运行破坏性测试。
- 前端再次执行：**9 个文件 / 28 个测试通过**，类型检查与构建通过，lint **0 错误 / 137 条风格警告**；
  四个 Python 网关 fixture 测试通过，ResourceWarning 作为错误处理。
- 全部 **13 份主规格严格校验通过**；Git 内容检查增加本机正常启动时独立生成的 MCP 密钥，
  共比对 11 个实际配置值及常见编码形式，未发现泄露。私密运行配置、备份均位于仓库外，
  原 `.env`、依赖、构建产物及验收报告不纳入提交。
