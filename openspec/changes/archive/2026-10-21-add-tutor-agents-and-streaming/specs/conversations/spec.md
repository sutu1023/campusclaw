# Spec Delta

## Purpose

用会话容纳一次连续追问、用消息记录说过的每一句与助手侧的出处，并让一个会话从建立起固定使用一个智能体，使多轮回答始终回到当时的角色设定与绑定。

## ADDED Requirements

### Requirement: 会话与消息数据模型

系统 MUST 以 `conversations` 保存会话，字段包含 `id`、`class_id`、`user_id`、`agent_id`、`title`、`created_at`、`updated_at`；并 MUST 以 `messages` 按写入顺序保存每一句，字段包含 `id`、`conversation_id`、`role`、`content`、`citations_json`、`skill_name`、`created_at`。`role` MUST 只允许 `user` 与 `assistant` 两个取值，并由数据库约束。会话行 MUST NOT 保存提示词或对话正文；对话正文与出处 MUST 只落在消息行。`conversations.class_id` MUST 取自登录用户，MUST NOT 由请求指定。

#### Scenario: 会话与消息按外键关联

- **WHEN** 为某会话写入多条消息
- **THEN** 每条消息的 `conversation_id` MUST 关联到该会话
- **AND** 同一会话内的消息 MUST 能按 `id` 升序还原写入顺序

#### Scenario: 角色取值受限

- **WHEN** 尝试写入 `role` 不是 `user` 或 `assistant` 的消息
- **THEN** 数据库约束 MUST 拒绝该写入

#### Scenario: 删除会话级联消息

- **WHEN** 删除一条会话
- **THEN** 其全部消息 MUST 一并删除

#### Scenario: 会话只记归属不记正文

- **WHEN** 检查一条会话行
- **THEN** 该行 MUST 只含归属、选定的智能体与标题等元数据
- **AND** MUST NOT 含提示词或任何一轮对话正文

### Requirement: 新建会话选定智能体

系统 MUST 要求新建会话时指定一个智能体，并 MUST 校验它属于本班且处于启用状态；校验不通过（不存在、他班或未启用）MUST 一律按「不存在」返回 404。校验通过后系统 MUST 把该编号写入会话行，此后该会话一直使用它，请求中再携带别的智能体编号 MUST NOT 更换它；要更换 MUST 新建会话。本班当时没有任何已启用智能体时，新建会话 MUST 返回 400。

#### Scenario: 按本班已启用智能体新建成功

- **WHEN** 学生以本班一个已启用智能体的编号新建会话
- **THEN** 系统 MUST 创建会话并记录该 `agent_id`
- **AND** 响应 MUST 返回该会话

#### Scenario: 不存在 / 他班 / 未启用一律 404

- **WHEN** 用不存在、属于他班或已停用的智能体编号新建会话
- **THEN** 系统 MUST 返回 404
- **AND** 三种情形的响应 MUST 完全一致

#### Scenario: 本班没有已启用智能体返回 400

- **WHEN** 本班当前没有任何已启用智能体，用户尝试新建会话
- **THEN** 系统 MUST 返回 400
- **AND** 错误信息 MUST 表明本班暂无可用智能体

#### Scenario: 会话建立后不随请求更换智能体

- **WHEN** 某会话已选定智能体 A，后续提问的请求体里携带智能体 B 的编号
- **THEN** 该轮回答 MUST 仍按 A 的提示词与技能执行
- **AND** 会话的 `agent_id` MUST 保持不变

#### Scenario: 智能体停用不影响已有会话

- **WHEN** 会话选定的智能体随后被教师停用
- **THEN** 该已有会话 MUST 仍可继续提问
- **AND** 停用 MUST 只拦住新建会话时的选用

### Requirement: 会话标题与更新时间

新建会话时标题 MUST 写入占位串「新的提问」。首轮提问到达后，系统 MUST 用该提问的前 24 个字符覆盖标题；截取结果为空时 MUST 保留占位串。此后每轮提问 MUST 刷新 `updated_at`，会话列表 MUST 按 `updated_at` 倒序返回。

#### Scenario: 新建时使用占位标题

- **WHEN** 新建一个尚未提问的会话
- **THEN** 其标题 MUST 为「新的提问」

#### Scenario: 首轮提问改写标题

- **WHEN** 会话收到首轮提问且提问非空
- **THEN** 系统 MUST 用该提问前 24 个字符覆盖标题

#### Scenario: 空提问保留占位标题

- **WHEN** 首轮提问为空或截取结果为空
- **THEN** 标题 MUST 保持占位串

#### Scenario: 列表按最近提问排序

- **WHEN** 用户拥有多个会话并最近向其中一个提问
- **THEN** 列表 MUST 按 `updated_at` 倒序返回

### Requirement: 上下文窗口

系统 MUST 在每轮回答时重新查询消息表，按写入顺序取**末尾 6 条**消息送入上下文，并把每条正文截断到 **500 个字符**；`role` 不是 `user` 或 `assistant` 的记录、以及正文为空的记录 MUST 跳过。窗口 MUST 固定，MUST NOT 随会话变长而增长；更早的消息 MUST 仍保留在库中，只是不再进入上下文。本轮提问 MUST 在生成之前写入，因此 MUST 落在窗口内。

#### Scenario: 只取末尾 6 条

- **WHEN** 某会话已有超过 6 条消息后再次提问
- **THEN** 送入上下文的历史 MUST 只含最近 6 条
- **AND** 更早的消息 MUST 不再进入上下文且仍保留在库中

#### Scenario: 每条正文截断 500 字

- **WHEN** 窗口内某条消息正文超过 500 个字符
- **THEN** 送入上下文的该条正文 MUST 被截断到 500 个字符

#### Scenario: 跳过非法角色与空正文

- **WHEN** 消息表中存在角色不是 `user`/`assistant` 或正文为空的记录
- **THEN** 这些记录 MUST 被跳过而不计入窗口

#### Scenario: 本轮提问必在窗口内

- **WHEN** 处理一轮提问
- **THEN** 该提问 MUST 已先写入消息表
- **AND** MUST 出现在本轮读取的窗口内

### Requirement: 会话归属与跨班隔离

会话与消息是私有资源。读取会话列表、读取单条会话与新建会话 MUST 先按当前用户过滤，再比对班级；任一不符 MUST 返回与「不存在」完全一致的 404。未登录访问 MUST 返回 401 且 MUST NOT 清空任何数据。两重过滤缺一不可，防的是对象级越权与以状态码差异探测资源存在性。

#### Scenario: 列表只含本人会话

- **WHEN** 用户列出会话
- **THEN** 返回集合中每条会话的 `user_id` MUST 等于当前用户
- **AND** MUST NOT 出现他人的会话

#### Scenario: 读取他人会话表现为不存在

- **WHEN** 用户按编号读取他人创建的会话
- **THEN** 系统 MUST 返回 404
- **AND** 响应 MUST 与读取一个不存在编号时完全一致

#### Scenario: 读取他班会话表现为不存在

- **WHEN** 用户按编号读取属于他班的会话
- **THEN** 系统 MUST 返回 404
- **AND** 响应 MUST 与读取一个不存在编号时完全一致

#### Scenario: 未登录返回 401

- **WHEN** 未携带令牌访问会话接口
- **THEN** 系统 MUST 返回 401
- **AND** MUST NOT 清空任何会话或消息数据
