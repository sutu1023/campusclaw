# Spec Delta

## MODIFIED Requirements

### Requirement: 用户登录

系统 MUST 提供教师（teacher）、学生（student）两类角色的账号密码登录；登录成功后 MUST 在服务端会话表（`sessions`）建立会话并**换发新的会话标识**，Cookie MUST 仅携带该随机会话标识且 MUST 为 HttpOnly；用户、角色与班级 MUST 保存在服务端。未登录用户访问受保护页面 MUST 被引导到登录页，且 MUST NOT 泄露任何业务数据。登录失败时，账号不存在、口令错误等不同原因对外 MUST 使用相同的状态码与文案。

#### Scenario: 教师与学生登录成功

- **WHEN** 用户使用有效账号与密码发起登录（预置账号含教师 A、学生 A1、学生 B1）
- **THEN** 登录 MUST 成功并建立服务端会话
- **AND** MUST 在服务端保存该用户的标识、角色（teacher 或 student）与所属班级（如班级 A 或 B）
- **AND** 下发的 Cookie MUST 仅包含随机会话标识，MUST NOT 包含角色或班级

#### Scenario: 登录成功换发会话标识

- **WHEN** 身份校验通过，浏览器此前已携带旧会话标识
- **THEN** 系统 MUST 签发新的会话标识
- **AND** 旧标识 MUST 立即失效，持旧标识的请求 MUST 被拒绝

#### Scenario: 错误密码登录失败

- **WHEN** 用户提供存在用户名但错误密码
- **THEN** 登录 MUST 失败且 MUST NOT 建立有效会话
- **AND** 对外响应 MUST 与账号不存在时一致（相同状态码与文案）
- **AND** 响应 MUST NOT 返回密码哈希或其他敏感信息

#### Scenario: 未登录访问受保护页面

- **WHEN** 未携带有效会话的用户请求受保护页面（如材料列表）或受保护 API
- **THEN** 对页面请求 MUST 重定向到登录页
- **AND** 对 API 请求 MUST 返回未授权（如 HTTP 401，响应体为统一文案）
- **AND** 响应 MUST NOT 包含任何本班或他班材料标题、正文或文件路径

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

系统 MUST 使用单向密码哈希算法存储用户密码，禁止明文；会话签名/标识相关密钥与数据库连接口令 MUST 仅通过服务端环境变量或 Compose 注入，MUST NOT 硬编码在源码或提交到版本库。`GET /health` MUST 不要求认证且 MUST NOT 返回材料内容。

#### Scenario: 库中无明文密码

- **WHEN** 直接查询用户表中密码相关字段
- **THEN** 字段值 MUST NOT 等于任何已知预置账号的明文口令
- **AND** MUST 可识别为哈希格式（如 bcrypt `$2` 前缀或 argon2 标识）

#### Scenario: 使用正确密码登录通过

- **WHEN** 用户使用正确明文密码登录
- **THEN** 系统 MUST 通过哈希验证并建立服务端会话

#### Scenario: 使用错误密码登录被拒

- **WHEN** 同一用户使用错误密码登录
- **THEN** 验证 MUST 失败且不建立会话

#### Scenario: 缺少会话密钥时服务不得静默使用默认值

- **WHEN** 生产/Compose 配置未提供会话或数据库必需的敏感环境变量
- **THEN** 应用启动 MUST 失败，或使用文档明确禁止的不安全默认值
- **AND** `.env.example` MUST 列出所需变量名

### Requirement: Docker Compose 部署与健康检查

系统 MUST 以 Docker Compose 作为标准启动方式，包含 **Nginx 入口、Go 服务、MySQL 与 Qdrant**；浏览器 MUST 仅访问 Nginx 入口，MySQL MUST NOT 映射宿主机端口，Qdrant 与模型网关 MUST 仅由 Go 服务在服务端访问。应用 MUST 提供 `GET /health`；数据库数据与上传目录 MUST 通过 volume 持久化，容器重建后数据仍在。

#### Scenario: Compose 启动后可访问

- **WHEN** 操作者按 README 复制 `.env.example`、填好必需变量并执行 `docker compose up --build` 直至 healthcheck 通过
- **THEN** 浏览器 MUST 可访问登录页 URL（经 Nginx）
- **AND** `GET /health` MUST 返回 HTTP 200 且 body 表明服务可用（如 JSON `status: ok`）

#### Scenario: 数据库不对宿主暴露且客户端无法直连

- **WHEN** 检查 Compose 网络与端口映射
- **THEN** MySQL MUST NOT 映射宿主机端口
- **AND** Qdrant 与模型网关 MUST NOT 作为浏览器入口

#### Scenario: health 不依赖登录态

- **WHEN** 未携带会话 cookie 的请求访问 `GET /health`
- **THEN** MUST 仍返回成功响应
- **AND** MUST NOT 重定向到登录页

#### Scenario: 重建容器后数据仍在

- **WHEN** 已存在预置用户、材料或上传产生的记录后，执行 `docker compose down` 再 `docker compose up` 且未删除数据 volume
- **THEN** 预置班级与用户 MUST 仍可登录
- **AND** 已上传材料与知识库正文 MUST 仍可通过本班列表或 SQL 查询到
