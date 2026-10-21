# Spec Delta

## MODIFIED Requirements

### Requirement: 前端页面范围与角色渲染

SPA MUST 至少覆盖登录、本班材料列表与上传、材料详情、检索与问答、智能体管理、技能管理、会话列表与新建、以及会话中的流式聊天界面，并 MUST 按服务端返回的角色与班级渲染（教师可见上传、智能体与技能的写入口，学生仅可列出与选用，MUST NOT 展示写入口）；前端隐藏入口 MUST NOT 被视为权限控制手段，服务端 MUST 独立校验；SPA MUST NOT 允许学生在界面上修改提示词、技能或绑定。

#### Scenario: 按角色渲染

- **WHEN** 教师登录
- **THEN** SPA MUST 展示材料上传入口以及智能体与技能的创建 / 修改 / 启停入口
- **AND** WHEN 学生登录
- **THEN** SPA MUST NOT 展示材料上传入口或智能体与技能的写入口
- **AND** 服务端仍 MUST 拒绝学生的写请求

#### Scenario: 覆盖本课新增界面

- **WHEN** 已登录用户浏览 SPA
- **THEN** SPA MUST 提供智能体列表与（教师）编辑、技能列表与（教师）编辑、会话列表与新建会话、以及会话内提问的界面
- **AND** 新建会话时 MUST 能在本班已启用智能体中选择一个

#### Scenario: 学生不能改配置

- **WHEN** 学生登录后查看智能体或技能
- **THEN** SPA MUST NOT 提供修改提示词、技能或绑定的入口
- **AND** 学生仍 MUST 能列出本班技能与已启用智能体并在新建会话时选用

## ADDED Requirements

### Requirement: 流式会话界面

聊天界面 MUST 以 `fetch` 向会话提问接口发起普通 POST（而不是只能发 GET 的 `EventSource`），再读取响应的流并自行按 `event:` 与 `data:` 行解帧。界面 MUST 在 `meta` 事件到达时即可显示本次引用列表与技能，并 MUST 把各次 `delta` 的新增文本逐段追加渲染，使文字逐步出现而非一次性出现；`done` 到达后 MUST 能打开引用材料，`error` 到达后 MUST 以通用文案收尾。引用材料 MUST 先转义再渲染，MUST NOT 执行其中的脚本。

#### Scenario: 逐段渲染

- **WHEN** 学生在会话中提交一次提问
- **THEN** 界面 MUST 在回答生成过程中逐段追加文本
- **AND** MUST NOT 等整段回答生成完毕才一次性渲染

#### Scenario: 引用先行展示

- **WHEN** 收到 `meta` 事件
- **THEN** 界面 MUST 立即展示本次技能与引用列表
- **AND** 正文随后逐段追加

#### Scenario: 结束后可打开引用材料

- **WHEN** 收到 `done` 事件
- **THEN** 界面 MUST 保留可据此打开对应材料的引用信息

#### Scenario: 错误事件收尾

- **WHEN** 收到 `error` 事件
- **THEN** 界面 MUST 以通用错误文案结束本次渲染
- **AND** MUST NOT 丢失已渲染的文本

#### Scenario: 引用内容不执行脚本

- **WHEN** 引用材料标题或片段含脚本类内容
- **THEN** 界面 MUST 转义后渲染
- **AND** MUST NOT 执行其中的脚本
