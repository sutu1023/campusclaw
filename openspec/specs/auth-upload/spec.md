# auth-upload Specification

## Purpose

让教师与学生以各自角色登录 CampusClaw，以班级作为数据边界隔离教学材料；教师上传的内容解析后写入知识库，本班成员通过列表查看；越权上传、跨班访问与未登录访问在服务端被拒绝。

## Requirements

### Requirement: 用户登录

系统 MUST 提供教师（teacher）、学生（student）两类角色的账号密码登录；登录成功后 MUST 在服务端 `sessions` 表建立会话并**签发新的高熵不透明令牌**，令牌 MUST 仅通过响应体返回客户端，MUST NOT 下发到 Cookie；客户端 MUST 在后续请求的 `Authorization: Bearer <token>` 头中携带该令牌。用户、角色与班级 MUST 保存在服务端。未登录或令牌缺失、无效、过期时访问受保护资源 MUST 被拒绝，且 MUST NOT 泄露任何业务数据。登录失败时，账号不存在、口令错误等不同原因对外 MUST 使用相同的状态码与文案。

#### Scenario: 教师与学生登录成功

- **WHEN** 用户使用有效账号与密码发起登录（预置账号含教师 A、学生 A1、学生 B1）
- **THEN** 登录 MUST 成功并在服务端建立会话
- **AND** 响应 MUST 返回新的不透明令牌
- **AND** 服务端 MUST 保存该用户的标识、角色（teacher 或 student）与所属班级（如班级 A 或 B）
- **AND** 系统 MUST NOT 依赖 Cookie 传递令牌或会话

#### Scenario: 登录成功换发令牌

- **WHEN** 身份校验通过，客户端此前已持有旧令牌
- **THEN** 系统 MUST 签发新的令牌
- **AND** 旧令牌 MUST 立即失效，持旧令牌的请求 MUST 被拒绝

#### Scenario: 以 Bearer 令牌访问受保护资源

- **WHEN** 客户端在 `Authorization: Bearer <token>` 头中携带有效令牌请求受保护 API
- **THEN** 系统 MUST 由该令牌解析出用户、角色与班级
- **AND** MUST NOT 要求请求携带会话 Cookie

#### Scenario: 错误密码登录失败

- **WHEN** 用户提供存在用户名但错误密码
- **THEN** 登录 MUST 失败且 MUST NOT 签发有效令牌
- **AND** 对外响应 MUST 与账号不存在时一致（相同状态码与文案）
- **AND** 响应 MUST NOT 返回密码哈希或其他敏感信息

#### Scenario: 未登录访问受保护页面

- **WHEN** 未携带令牌，或令牌无效 / 过期，请求受保护页面或受保护 API
- **THEN** 对页面请求 MUST 引导到登录页
- **AND** 对 API 请求 MUST 返回未授权（如 HTTP 401，响应体为统一文案）
- **AND** 响应 MUST NOT 包含任何本班或他班材料标题、正文或文件路径

#### Scenario: 登出吊销令牌

- **WHEN** 用户以有效令牌请求登出
- **THEN** 系统 MUST 删除或吊销该令牌对应的服务端会话
- **AND** 持该令牌的后续请求 MUST 返回 401

### Requirement: 角色权限

系统 MUST 按服务端会话解析出的用户角色授权：教师可上传与管理本班材料；学生对本班材料只读；学生调用上传或管理接口 MUST 在文件落盘与数据写入之前被拒绝。角色 MUST 以服务端用户记录为准，MUST NOT 采信请求中自行声明的角色。

#### Scenario: 学生上传被拒绝（403）

- **WHEN** 以 student 角色（如学生 A1）的有效会话向材料上传接口提交文件
- **THEN** 系统 MUST 拒绝请求并返回 HTTP 403（或等价语义的错误码）
- **AND** `materials` 与知识库相关表 MUST 无新增或变更记录
- **AND** 上传目录 MUST 无因该请求产生的新文件

#### Scenario: 教师上传被允许

- **WHEN** 以 teacher 角色（如教师 A）的有效会话向材料上传接口提交支持的文件
- **THEN** 系统 MUST 接受请求并执行入库流程（见「材料上传与知识库入库」）
- **AND** 返回 MUST 表示成功（如 HTTP 201）并含新材料标识

#### Scenario: 学生仅只读本班列表

- **WHEN** 学生 A1 登录后打开本班材料列表
- **THEN** 系统 MUST 展示本班可读材料
- **AND** 页面或 API MUST NOT 提供学生可用的上传或删除本班材料的写操作入口（服务端仍 MUST 拒绝写 API）

### Requirement: 班级隔离

班级是数据边界。所有材料、知识库及相关业务查询 MUST 在服务端按会话中的班级标识过滤；A 班用户 MUST NOT 读取、修改或删除 B 班材料；前端隐藏按钮 MUST NOT 作为满足本要求的唯一手段。

#### Scenario: 跨班按 ID 访问材料被拒绝

- **WHEN** 班级 A 的用户（学生 A1 或教师 A）通过 URL、API 路径参数或请求体指定 B 班某条材料的 ID 发起访问
- **THEN** 系统 MUST 拒绝访问（HTTP 403 或 404，实现择一并在文档中固定）
- **AND** 响应 MUST NOT 返回 B 班材料的标题、正文片段、文件路径或存储键

#### Scenario: 跨班枚举列表不得泄露 B 班

- **WHEN** 学生 A1 登录后请求材料列表（含分页、搜索参数为空或任意合法值）
- **THEN** 返回集合中每一条记录的班级归属 MUST 均为班级 A
- **AND** MUST NOT 出现 B 班预置材料的可区分标题（如标题中含「B 班」的样本）

#### Scenario: 材料列表仅含本班记录

- **WHEN** 教师 A 登录后打开本班材料列表
- **THEN** 列表中每一条记录 MUST 归属于班级 A
- **AND** 列表数据 MUST 来自数据库查询且查询条件含班级 A 的过滤

### Requirement: 材料上传与知识库入库

教师上传教学材料后，系统 MUST 将文件落盘、解析文本并**保留上传原文**（`knowledge_entries.body_text`）；材料与知识库条目 MUST 关联上传者所属班级且 MUST 在同一事务中提交。上传成功后 MUST 触发正文切片与向量入库流程（见 `knowledge-retrieval`）。本班材料列表 MUST 从数据库查询展示；上传成功后刷新列表 MUST 可见新记录。上传是一次事务性操作：磁盘写入与两表写入同生共死，失败时 MUST 回退到「无文件、无记录」的干净状态。

#### Scenario: 上传后知识库与列表可查

- **WHEN** 教师 A 通过上传接口提交一份合法文件（如 `.md` 或 `.txt`）且解析成功
- **THEN** `materials` MUST 新增一条关联班级 A 的记录
- **AND** `knowledge_entries` MUST 新增一条关联该材料与班级 A 的正文记录，且 `body_text` MUST 为上传原文
- **AND** 系统 MUST 触发该正文的切片与向量入库流程
- **AND** 教师 A 刷新本班材料列表时 MUST 看到该条新记录

#### Scenario: 上传后学生本班可见只读

- **WHEN** 教师 A 成功上传新材料后，学生 A1 刷新本班材料列表
- **THEN** 学生 A1 MUST 能在列表中看到该条新材料
- **AND** 学生 A1 对该材料 MUST 仍为只读（不得通过 API 修改或覆盖）

#### Scenario: 上传失败时不产生脏数据

- **WHEN** 上传过程因不支持格式、超限大小、空文件或解析失败而终止
- **THEN** 系统 MUST 返回明确错误（如 HTTP 400）
- **AND** MUST NOT 在材料表或知识库表中留下不完整记录
- **AND** MUST NOT 保留孤立的未关联文件

### Requirement: 预置核心数据

系统 MUST 建立 **五类核心数据结构**：班级、用户、会话、材料与知识库正文（`classes`、`users`、`sessions`、`materials`、`knowledge_entries`），其中 `materials` 与 `knowledge_entries` MUST 携带非空且带索引的租户列 `class_id`。系统 MUST 预置可验收的样本数据，以支持登录、班级隔离与上传验收。

#### Scenario: 种子数据满足双班与用户

- **WHEN** 首次执行数据库初始化或种子脚本（含 Compose 首次启动）
- **THEN** 库中 MUST 存在班级 A、班级 B
- **AND** MUST 存在教师 A（归属班级 A）、学生 A1（归属 A）、学生 B1（归属 B）
- **AND** 每个账号 MUST 具备可登录的用户名与已哈希的密码字段

#### Scenario: 两班材料标题可区分

- **WHEN** 种子脚本执行完成
- **THEN** MUST 存在至少一条明确归属 A 班的材料标题（如含「A 班」）
- **AND** MUST 存在至少一条明确归属 B 班的材料标题（如含「B 班」）
- **AND** 至少一条材料 MUST 具备可供检索的正文与切片（或可由重建索引补齐）

#### Scenario: 租户列非空且可索引

- **WHEN** 检查 `materials` 与 `knowledge_entries` 表结构
- **THEN** 两表的 `class_id` MUST 为非空列
- **AND** `class_id` 上 MUST 存在索引

### Requirement: 密码哈希与会话密钥

系统 MUST 使用单向密码哈希算法存储用户密码，禁止明文；令牌哈希所需密钥（`TOKEN_SECRET`）与数据库连接口令 MUST 仅通过服务端环境变量或 Compose 注入，MUST NOT 硬编码在源码或提交到版本库；服务端 MUST NOT 保存明文令牌。`GET /health` MUST 不要求认证且 MUST NOT 返回材料内容。

#### Scenario: 库中无明文密码

- **WHEN** 直接查询用户表中密码相关字段
- **THEN** 字段值 MUST NOT 等于任何已知预置账号的明文口令
- **AND** MUST 可识别为哈希格式（如 bcrypt `$2` 前缀或 argon2 标识）

#### Scenario: 使用正确密码登录通过

- **WHEN** 用户使用正确明文密码登录
- **THEN** 系统 MUST 通过哈希验证并签发服务端会话令牌

#### Scenario: 使用错误密码登录被拒

- **WHEN** 同一用户使用错误密码登录
- **THEN** 验证 MUST 失败且不签发有效令牌

#### Scenario: 库中无明文令牌

- **WHEN** 直接查询 `sessions` 表
- **THEN** MUST NOT 存在明文令牌值
- **AND** 存储值 MUST 为令牌的哈希（如 HMAC-SHA256 结果）

#### Scenario: 缺少会话密钥时服务不得静默使用默认值

- **WHEN** 生产/Compose 配置未提供令牌哈希或数据库必需的敏感环境变量
- **THEN** 应用启动 MUST 失败，或使用文档明确禁止的不安全默认值
- **AND** `.env.example` MUST 列出所需变量名

### Requirement: Docker Compose 部署与健康检查

系统 MUST 以 Docker Compose 作为标准启动方式，包含 **Nginx 入口、Go 服务、MySQL 与 Qdrant，并由 Nginx 托管前端 SPA 构建产物**；浏览器 MUST 仅访问 Nginx 入口，MySQL MUST NOT 映射宿主机端口，Qdrant 与模型网关 MUST 仅由 Go 服务在服务端访问。应用 MUST 提供 `GET /health`；数据库数据与上传目录 MUST 通过 volume 持久化，容器重建后数据仍在。

#### Scenario: Compose 启动后可访问

- **WHEN** 操作者按 README 复制 `.env.example`、填好必需变量并执行 `docker compose up --build` 直至 healthcheck 通过
- **THEN** 浏览器 MUST 可访问由 Nginx 托管的 SPA 入口与登录页
- **AND** `GET /health` MUST 返回 HTTP 200 且 body 表明服务可用（如 JSON `status: ok`）

#### Scenario: SPA 由 Nginx 托管

- **WHEN** 浏览器请求应用根路径或深层前端路由（如 `/materials`、`/search`）
- **THEN** Nginx MUST 返回 SPA 入口页面而非 404
- **AND** 前端静态资源 MUST 由 Nginx 提供
- **AND** Go 服务 MUST NOT 渲染业务 HTML 模板

#### Scenario: 数据库不对宿主暴露且客户端无法直连

- **WHEN** 检查 Compose 网络与端口映射
- **THEN** MySQL MUST NOT 映射宿主机端口
- **AND** Qdrant 与模型网关 MUST NOT 作为浏览器入口

#### Scenario: health 不依赖登录态

- **WHEN** 未携带令牌的请求访问 `GET /health`
- **THEN** MUST 仍返回成功响应
- **AND** MUST NOT 引导到登录页

#### Scenario: 重建容器后数据仍在

- **WHEN** 已存在预置用户、材料或上传产生的记录后，执行 `docker compose down` 再 `docker compose up` 且未删除数据 volume
- **THEN** 预置班级与用户 MUST 仍可登录
- **AND** 已上传材料与知识库正文 MUST 仍可通过本班列表或 SQL 查询到
