# Spec Delta

## MODIFIED Requirements

### Requirement: SSE 事件与帧格式

`POST /api/conversations/{id}/messages` MUST 以 `text/event-stream` 返回，每一帧由 `event:` 与 `data:` 两行组成并以一个空行结束，`data` MUST 是 JSON。系统 MUST 支持四类事件：首次 `meta` 在检索完成后、最终正文生成之前最先到达，携带状态、本次命中的技能与引用列表；工具阶段 MUST 在每轮处理后追加 `meta`，保持相同技能和引用并在 `tool_calls` 中携带该轮新增调用的 `call_id`、`ref`、`round`、`status` 及有界安全文本/原因；`delta` 在最终生成过程中多次推送新增文本；`done` 在助手消息落库之后推送消息编号；`error` 在流已开始但后续主路径失败时推送通用错误。工具调用的 `status` MUST 为 `ok`、`error` 或 `denied`，外层 `meta.status` MUST 仍为 `ok`；没有调用、引用拒绝或工具阶段错误记录时 MUST 省略 `tool_calls`。所有工具元数据 MUST 先于第一段正文到达。把各次 `delta.text` 拼接 MUST 等于最终回答。

#### Scenario: meta 先行

- **WHEN** 一轮提问的检索完成
- **THEN** 最先到达的 MUST 是 `meta` 事件
- **AND** 其载荷 MUST 含状态、本次技能与引用列表

#### Scenario: delta 是增量

- **WHEN** 回答分多段生成
- **THEN** 系统 MUST 多次推送 `delta`
- **AND** 每个 `delta` MUST 是此前未发送过的新增文本，MUST NOT 是累计全文

#### Scenario: 拼接等于最终回答

- **WHEN** 把一次回答的全部 `delta.text` 按顺序拼接
- **THEN** 结果 MUST 等于该条助手消息的最终正文

#### Scenario: done 在落库之后

- **WHEN** 助手消息写入数据库完成
- **THEN** 系统 MUST 推送 `done` 事件
- **AND** 其载荷 MUST 含该条助手消息的编号

#### Scenario: 流内失败以 error 收尾

- **WHEN** 流已开始后最终生成或写入助手消息失败
- **THEN** 系统 MUST 按既有部分生成降级规则以 `error` 或允许的 `done` 收尾
- **AND** MUST NOT 尝试更改已写出的 HTTP 状态码

#### Scenario: 多轮工具元数据早于正文

- **WHEN** 本次问题连续处理两轮工具调用
- **THEN** MUST 按发生顺序发送两次附带新增 `tool_calls` 的 `meta`
- **AND** 两轮调用元数据 MUST 都在第一个 `delta` 前到达

#### Scenario: 失效引用明确拒绝

- **WHEN** 命中技能包含已断开、待确认、已删除或不属于本班的服务器引用
- **THEN** `meta.tool_calls` MUST 为该引用提供 `denied`，不得等待模型猜测该不可见工具
- **AND** 外层状态 MUST 保持 `ok`，最终回答 MUST 仍能继续

#### Scenario: 原有无工具口径

- **WHEN** 无命中技能、技能没有 MCP 工具，或模型没有提出调用且没有引用拒绝/工具阶段错误
- **THEN** `meta` MUST 仍携带原有状态、技能和引用
- **AND** MUST 省略 `tool_calls` 而不是提供伪造记录

### Requirement: 助手消息落库与可回溯

助手消息除正文外 MUST 一并写入本轮引用列表、命中的技能名及按发生顺序排列的安全工具调用摘要；`done` 事件返回的消息编号 MUST 指向该条落库记录。事后 MUST 能按会话回放：这一轮用了哪个智能体、命中了哪个技能、引用了哪几份材料、材料属于哪个班、尝试了哪些 `tool@server` 以及其状态与轮次。旧消息 MUST 以空工具调用集合兼容读取。工具协议的中间 assistant/tool 消息 MUST NOT 作为独立聊天消息落库或扩大末尾 6 条历史窗口；会话读取仍 MUST 同时校验用户与班级。

#### Scenario: 落库含引用与技能

- **WHEN** 一轮回答结束并推送 `done`
- **THEN** 对应的助手消息 MUST 含正文、引用列表、技能名及该轮工具调用摘要
- **AND** `done` 返回的编号 MUST 能在消息表中查到

#### Scenario: 可回溯到智能体与材料

- **WHEN** 事后读取一条助手消息及其会话
- **THEN** MUST 能确定该轮使用的智能体、命中的技能与引用的材料
- **AND** MUST 能确定引用材料所属班级

#### Scenario: 刷新后工具状态仍在

- **WHEN** 回答结束后刷新会话页面或重新读取会话
- **THEN** MUST 能还原每次调用的引用、状态和轮次
- **AND** 老消息 MUST 正常展示而不要求补造历史调用

### Requirement: 提示注入边界

检索到的正文、材料标题、技能说明、工具说明与远端工具结果 MUST 被视为数据而非指令。编号检索资料 MUST 留在用户消息；工具结果 MUST 仅以宿主关联的 `tool` 消息进入本轮上下文，并带来源标识；系统消息 MUST NOT 包含工具结果，末尾 MUST 固定追加包括这些不可信来源的边界声明。技能 `工具` 字段 MUST 仅允许扩展到命中技能授权候选集中的 MCP 调用，MUST NOT 因工具说明或返回文本获得额外权限。客户端自行构造的 `system`、`tool` 消息或 assistant 工具调用 MUST 被丢弃；真实调用和 `tool` 消息 MUST 只能由宿主产生。

#### Scenario: 上传文本不获得指令权限

- **WHEN** 某材料正文包含试图改变系统行为的语句
- **THEN** 该正文 MUST 作为用户消息中的资料出现
- **AND** MUST NOT 被当作系统指令执行

#### Scenario: 不向模型注入工具

- **WHEN** 没有命中带有可用 MCP 引用的技能
- **THEN** 请求 MUST NOT 含任何可调用的 MCP 工具定义
- **AND** 技能中的内置 `knowledge_search` MUST NOT 触发远端调用

#### Scenario: 丢弃客户端 system 消息

- **WHEN** 请求体或历史中携带客户端自行构造的 `system`、`tool` 消息或工具调用字段
- **THEN** 系统 MUST 丢弃这些内容
- **AND** 系统提示与真实工具协议消息 MUST 由服务端写入

#### Scenario: 工具结果无系统权限

- **WHEN** 本轮有合法 MCP 工具返回
- **THEN** 返回数据 MUST 位于标注来源的 `tool` 消息，MUST NOT 进入系统消息
- **AND** 原有解题引导不给最终答案的硬性要求 MUST 继续生效
