# Spec Delta

## MODIFIED Requirements

### Requirement: 前端页面范围与角色渲染

SPA MUST 至少覆盖登录、本班材料列表与上传、材料详情、检索与问答、智能体管理、技能管理、会话列表与新建、会话中的流式聊天、以及教师专用 MCP 服务器/工具目录/审计界面，并 MUST 按服务端返回的角色与班级渲染。教师可见上传、智能体、技能与 MCP 管理入口；学生 MUST NOT 看到这些写入口或 MCP 管理入口，但 MUST 能在自己的会话中看到工具使用状态。前端隐藏入口 MUST NOT 被视为权限控制手段，服务端 MUST 独立校验；SPA MUST NOT 允许学生修改提示词、技能或绑定。

#### Scenario: 按角色渲染

- **WHEN** 教师登录
- **THEN** SPA MUST 展示材料上传、智能体与技能写入口及 MCP 管理/审计入口
- **AND** WHEN 学生登录
- **THEN** SPA MUST NOT 展示上述管理入口
- **AND** 服务端仍 MUST 拒绝学生的写请求和 MCP 管理请求

#### Scenario: 覆盖本课新增界面

- **WHEN** 已登录用户浏览 SPA
- **THEN** SPA MUST 提供智能体列表与教师编辑、技能列表与教师编辑、会话列表与新建、会话内提问界面，并为教师提供 MCP 管理界面
- **AND** 新建会话时 MUST 能在本班已启用智能体中选择一个

#### Scenario: 学生不能改配置

- **WHEN** 学生登录后查看智能体或技能
- **THEN** SPA MUST NOT 提供修改提示词、技能或绑定的入口
- **AND** 学生仍 MUST 能列出本班技能与已启用智能体并在新建会话时选用

### Requirement: 流式会话界面

聊天界面 MUST 以 `fetch` 向会话提问接口发起普通 POST，再读取响应流并按 `event:` 与 `data:` 解帧。界面 MUST 在首次 `meta` 到达时显示技能与引用；收到后续 `meta.tool_calls` MUST 按 `call_id` 去重、按轮次与发生顺序合并显示引用、状态和安全文本，MUST NOT 清空先前工具记录或重置已显示的引用。工具状态 MUST 在正文首字前可见，没有工具记录时 MUST 隐藏工具栏。`delta` MUST 逐段追加；`done` 后 MUST 保留引用和工具记录；`error` MUST 以通用文案收尾。刷新后 MUST 从会话消息恢复工具摘要。引用与工具文本 MUST 先转义后展示，MUST NOT 执行其中的脚本。

#### Scenario: 逐段渲染

- **WHEN** 学生在会话中提交一次提问
- **THEN** 界面 MUST 在最终回答生成过程中逐段追加文本
- **AND** MUST NOT 等整段回答生成完毕才一次性渲染

#### Scenario: 引用先行展示

- **WHEN** 收到 `meta` 事件
- **THEN** 界面 MUST 立即展示本次技能与引用列表，并合并其中新增的工具状态
- **AND** 正文随后逐段追加

#### Scenario: 结束后可打开引用材料

- **WHEN** 收到 `done` 事件
- **THEN** 界面 MUST 保留可据此打开对应材料的引用信息及工具调用摘要

#### Scenario: 错误事件收尾

- **WHEN** 收到 `error` 事件
- **THEN** 界面 MUST 以通用错误文案结束本次渲染
- **AND** MUST NOT 丢失已渲染的文本或工具状态

#### Scenario: 引用内容不执行脚本

- **WHEN** 引用材料标题、片段或工具文本含脚本类内容
- **THEN** 界面 MUST 转义后渲染
- **AND** MUST NOT 执行其中的脚本

#### Scenario: 多轮与刷新不丢记录

- **WHEN** 连续收到两轮工具 `meta` 并在回答结束后刷新
- **THEN** 界面 MUST 在流中和回放时都显示两轮记录
- **AND** 同一 `call_id` 的重复事件 MUST NOT 产生重复展示

## ADDED Requirements

### Requirement: 教师 MCP 管理与技能辅助

教师界面 MUST 支持登记名称与 Base URL、修改地址、连接/发现、断开、删除、查看完整工具说明和参数结构、确认变更快照、查看本班倒序分页审计及错误原因。凭据只由运维在服务端环境配置，界面 MUST 只显示是否配置，MUST NOT 输入、接收或持久化真实 MCP 密钥。技能编辑 MUST 展示 MCP 引用格式和可用工具参考，并原样显示后端保存校验错误，MUST NOT 仅在前端判断授权。

#### Scenario: 从发现到绑定

- **WHEN** 教师连接部署中的 MCP 服务并进入技能编辑
- **THEN** MUST 能看到 `formula_hint` 和 `unit_convert` 的完整说明、结构及引用写法
- **AND** 保存失败 MUST 显示后端可读原因，成功后 MUST 能读回绑定

#### Scenario: 变更确认与断开

- **WHEN** 重连出现变更快照或教师断开服务
- **THEN** 界面 MUST 明确显示待确认或已断开状态
- **AND** MUST NOT 继续把对应工具显示为可调用
