# Tasks

## 1. 存储层：表结构与种子

- [x] 1.1 在 `internal/store/schema.go` 追加六张表的幂等 DDL：`tutor_agents`、`skills`、`tutor_agent_skills`、`tutor_agent_materials`、`conversations`、`messages`（字段与约束见 `tutor-agents` / `ai-skills` / `conversations` spec）。验证：对已有库重复执行 `Migrate` 不报错，`SHOW TABLES` 含六张新表。
- [x] 1.2 为绑定表加唯一约束与外键级联（删除智能体清其绑定，删除技能 / 材料清对应绑定），并为 `conversations.agent_id`、`messages.conversation_id`、各 `class_id` 建索引。验证：`store` 单元测试覆盖删除技能后绑定行数为 0、删除会话后消息行数为 0。
- [x] 1.3 扩展 `Seed`：为班级 A 预置数学与语文「解题引导」技能各一条，写入 `seed_key` 并以 `INSERT ... ON DUPLICATE KEY UPDATE` 保证幂等；教师自建技能 `seed_key` 为 `NULL`。验证：连续执行两次 `Seed` 后 `SELECT COUNT(*) FROM skills` 不增加（`store` 单测）。
- [x] 1.4 实现智能体的读写方法：`CreateAgent`（写主表 + 两张绑定表，同一事务）、`ListAgents`（学生仅 `enabled=1`）、`GetAgent`、`UpdateAgent`（整体替换绑定）、`ListAgentCandidateSkills`（绑定且启用）、`ListAgentMaterials`。验证：单测覆盖创建带回读 `skill_ids`/`material_ids`、更新绑定后回读即为新关系。
- [x] 1.5 实现技能的读写方法：`CreateSkill`、`UpdateSkill`、`ListSkills`、`GetSkill`、`DeleteSkill`（同事务清绑定）。验证：单测覆盖创建带回读 `source_text`、删除后绑定清空。
- [x] 1.6 实现会话与消息方法：`CreateConversation`、`ListConversations`（按 `updated_at` 倒序）、`GetConversation`、`ListMessages`、`AppendMessage`、`RecentWindowMessages`（末尾 6 条、每条截断 500 字、跳过非法角色 / 空正文）。验证：单测覆盖窗口条数、截断长度、跳过规则与倒序。
- [x] 1.7 所有读取按 `class_id` 与 `user_id` 双重过滤，跨班 / 跨用户 / 不存在统一返回 `ErrNotFound`。验证：单测断言跨班读取智能体 / 技能 / 会话均得到 `ErrNotFound`，且不返回他班字段。

## 2. 技能解析与确定性路由

- [x] 2.1 新建 `internal/skill` 解析器：按首个空行切分元数据与正文；解析 `名称`、`适用条件`、`资料`、`工具`、`启用`，忽略 `#` 注释与空行；实现必填、重复字段、未定义字段、工具白名单（仅 `knowledge_search`）、正文非空、文件 / 字段长度上限的校验并返回可读错误。验证：表驱动单测覆盖 `ai-skills` spec 的全部被拒场景与合法场景。
- [x] 2.2 确认解析结果完整保留 `source_text` 并可回读。验证：单测对含注释与空行的文件做解析—回读 round-trip，断言原文逐字符一致。
- [x] 2.3 实现路由纯函数 `Route(question, candidates) *Skill`：中英文逗号 / 顿号 / 分号 / 竖线切词、两侧小写、子串计分、取最高、同分取 `id` 较小者、零分返回 `nil`。验证：表驱动单测覆盖候选过滤、最高分、同分、零分、大小写折叠与各分隔符（对应 `streaming-chat`「候选技能集与词法路由」场景）。
- [x] 2.4 实现资料范围过滤辅助：技能 `资料` 非空时只保留标题包含该词的材料。验证：单测断言标题含 / 不含该词的两类材料过滤结果正确。

## 3. 智能体与技能接口

- [x] 3.1 新增 `internal/httpapi/agents.go`：`GET /api/agents`、`POST /api/agents`、`GET /api/agents/{id}`、`PUT /api/agents/{id}`；写操作仅教师；绑定只接受本班技能 / 材料，跨班按不存在返回 404；`prompt` 空值可创建。验证：`httptest` 覆盖学生写 403 且无写入、教师创建 201、绑定他班 404、跨班读取 404 与不存在响应一致。
- [x] 3.2 新增 `internal/httpapi/skills.go`：`GET /api/skills`、`POST /api/skills`、`PUT /api/skills/{id}`、`DELETE /api/skills/{id}`；解析失败返回 400 且文案可读；班内同名 400；跨班 404；删除清绑定。验证：`httptest` 覆盖上述场景，并断言 400 文案包含缺失 / 非法字段信息。
- [x] 3.3 在 `Server.Router` 注册以上路由（读接口 `authed`，写接口叠加 `teacher`），并保持既有路由不变。验证：启动服务后 `curl` 未带令牌得到 401、学生令牌写得到 403、教师令牌得到 201。

## 4. 会话与消息接口

- [x] 4.1 新增 `internal/httpapi/conversations.go`：`POST /api/conversations`（校验本班已启用智能体，否则 404；本班无已启用返回 400）、`GET /api/conversations`、`GET /api/conversations/{id}`（返回会话与消息列表，消息含 `citations` 与 `skill_name`）。验证：`httptest` 覆盖创建成功、404 三种一致、400、列表只含本人、读取他人 / 他班 / 不存在均为一致 404。
- [x] 4.2 实现标题与时间逻辑：新建写「新的提问」，首轮提问改前 24 字（为空保留占位串），每轮刷新 `updated_at`。验证：`httptest` 覆盖占位标题、首轮改写、空提问保留、列表按最近提问倒序。
- [x] 4.3 接入历史窗口查询，向内提供服务返回末尾 6 条、每条 500 字。验证：单测构造 >6 条消息并断言窗口内容与长度。
- [x] 4.4 注册会话路由并接入 `auth.Middleware`。验证：未登录访问返回 401 且会话 / 消息数据不变。

## 5. 对话网关流式与消息组装

- [x] 5.1 在 `internal/chat` 增加流式方法：以 `stream: true` 调用网关，逐行解析 `data:` 增量并回调；网关返回非流式响应时把整段作为单个增量回调。验证：用 `httptest` 起一个假网关分别返回 SSE 与普通 JSON，断言回调次数与拼接结果。
- [x] 5.2 新增消息组装器：产出五段 system（提示词回退、技能说明与「解题引导」硬性要求 / 未命中提示、引用要求、不说明模型、边界声明）与 user（先剔除空正文 / 学科不符再连续编号、每条 800 字的编号资料 + 本轮提问）。验证：单测断言命中 / 未命中技能、有 / 无资料时的 system 与 user 文本，及编号连续性。
- [x] 5.3 实现无依据时的编号剥除（去除正文中的 `[n]`）与「丢弃客户端 system 消息」。验证：单测输入含 `[1]` 的模型输出断言已剥除；输入含客户端 `system` 消息断言被丢弃（对应 `streaming-chat`「提示注入边界」场景）。

## 6. SSE 流式回答接口

- [x] 6.1 新增流式处理器并注册 `POST /api/conversations/{id}/messages`：校验会话归属与班级，落库本轮提问，路由选技能，限定绑定材料检索取前 4 条，`meta` → `delta` × n → `done` 帧；响应头含 `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`Connection: keep-alive`、`X-Accel-Buffering: no`，每帧 `Flush`。验证：`httptest` 读取事件流，断言 `meta` 最先、多个 `delta` 且拼接等于最终正文、`done` 含消息编号。
- [x] 6.2 实现首事件前后的失败处理：首个事件前可返回 401 / 400 / 404 / 503 的 JSON，写出后状态固定 200、失败发 `error` 事件。验证：`httptest` 覆盖未登录 401、不存在会话 404、检索不可用 503 均非 `text/event-stream`；模拟中途失败得到 200 + `error`。
- [x] 6.3 落库助手消息：正文 + 引用列表 + 技能名，`done` 返回该消息编号。验证：`httptest` 读取 `GET /api/conversations/{id}`，断言助手消息含 `citations` 与 `skill_name`，且编号与 `done` 一致。
- [x] 6.4 实现生成中途失败降级：已有 `delta` 以部分内容落库收尾；无 `delta` 不落库并返回错误。验证：`httptest` 用假网关在第 N 个增量后报错，分别断言部分落库与空落库两种结果。
- [x] 6.5 集成验证编排口径：给智能体只绑数学技能与数学材料后问语文题——不命中技能、引用不含未绑定材料；命中「解题引导」时回答不写得数；检索落空时引用为空且正文无 `[n]`。验证：端到端测试或按 `scope.html` 验收表逐条手工验证并记录。

## 7. OpenAPI 与 Swagger

- [x] 7.1 为所有主要处理器添加 `swaggo/swag` 注解（摘要、路径、参数、请求 / 响应结构、错误码、`@Security BearerAuth`），并固化生成步骤（如 `make docs` / 脚本执行 `swag init`），提交生成物。验证：执行生成命令成功，`docs/swagger.json` 含本课新增接口。
- [x] 7.2 在 `Router` 中**于业务路由之前**挂载 `GET /openapi.json`（读取生成文档）与 `GET /swagger/index.html`（Swagger UI）。验证：浏览器打开 Swagger 页面、`curl /openapi.json` 返回可解析文档，且既有接口未受影响。
- [x] 7.3 核对文档与实现一致：每条注解的 `@Router` 路径与注册路径一致，列出 400 / 401 / 403 / 404 / 503 中实际可能出现的错误码，受保护接口标注 Bearer。验证：在 Swagger 页面上对登录 / 材料 / 智能体 / 会话 / 提问各发一次请求成功。
- [x] 7.4 更新 `README.md`：新增接口表（agents / skills / conversations / 流式提问 / openapi / swagger）、OpenAPI 生成命令与 Swagger 地址、SSE 说明。验证：按 README 的文档命令与地址实际操作成功。

## 8. 前端

- [x] 8.1 扩展 `web/src/api.ts` 与类型定义：智能体、技能、会话、消息的请求方法；复用 `apiFetch`（401 清 token 跳登录）。验证：`npm run typecheck` 通过。
- [x] 8.2 新增智能体管理视图：教师可创建 / 修改 / 启停并勾选本班技能与材料，学生只读列表并在新建会话时选用已启用项。验证：`npm run test` 中的组件 / 行为测试通过，并手动以教师 / 学生账号核对入口差异。
- [x] 8.3 新增技能管理视图：教师粘贴 / 上传技能文件、查看原文与解析字段、启停与删除，展示服务端 400 的可读错误。验证：手动提交非法文件看到错误文案，提交合法文件后列表出现该项。
- [x] 8.4 新增会话列表与新建视图：列出本人会话（按最近提问倒序）、选择本班已启用智能体新建、点击进入聊天。验证：手动新建会话后列表立即出现，标题为占位串且在首轮提问后改写。
- [x] 8.5 新增聊天视图：`fetch` POST 提交并读取 `response.body`，按 `event:` / `data:` 解帧，逐段追加 `delta`；`meta` 到达即显示技能与引用列表，`done` / `error` 收尾；引用内容转义后渲染。验证：`vitest` 单测覆盖解帧函数（多帧、跨 chunk 半帧、`error`），并手动确认文字逐段出现且可打开引用。
- [x] 8.6 更新 `web/src/router.ts` 与导航：注册新路由、按角色渲染菜单，学生不显示写入口。验证：`npm run lint` 与 `npm run build` 通过，手动刷新深层路由不 404。

## 9. 部署与 Nginx

- [x] 9.1 在 `deploy/nginx.conf` 为流式路径关闭代理缓冲并放宽超时：`proxy_buffering off`、`proxy_cache off`、`proxy_read_timeout 120s`，保留既有 `/api/` 与其他 location 规则。验证：经 Nginx 提交一次提问，浏览器抓包可见多个 `delta` 且非一次性返回；直接对比关闭该配置时退化为一次性返回。
- [x] 9.2 确认服务端与 Compose 无新增必填环境变量，密钥仍只来自本机 `.env`、不入库、不进镜像。验证：`docker compose config` 不含明文网关密钥，`.env.example` 未新增敏感默认值。

## 10. 接口测试与集成验收

- [x] 10.1 建立 `apitest/` 骨架：按 URL 组织脚本目录（如 `cases/api/login/POST.py`）、`run_all.py` 一键执行并生成自包含 HTML 报告，报告目录加入 `.gitignore`。验证：`python3 apitest/run_all.py` 生成 HTML 报告，且 `git status` 不显示报告文件。
- [x] 10.2 编写覆盖脚本：登录、检索、智能体与技能的创建 / 读取、新建会话并发一条消息（解析 SSE）。脚本除状态码外，回读 `GET /api/agents`、`GET /api/conversations/{id}` 与数据库核对内容一致。验证：`run_all.py` 全绿，报告含每项断言。
- [x] 10.3 回归第 3 / 4 课场景（登录、学生上传 403、跨班 404、检索与 `/api/ask` 固定文案）并执行第 5 课 DoD 与 `apply.html` 同伴交叉验证四项清单。验证：全绿并在本人验证记录中留存请求 / 响应证据。
- [x] 10.4 更新 `README.md` 的接口测试章节（`apitest/` 结构、`run_all.py` 用法、报告不入库说明）。验证：按 README 命令运行 `run_all.py` 成功生成报告。
