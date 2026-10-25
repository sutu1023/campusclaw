# Spec Delta

## MODIFIED Requirements

### Requirement: 技能文件格式与解析

技能 MUST 以单一文本文件定义：空行以上为元数据区，空行以下为引导规程。元数据区 MUST 支持 `名称`、`适用条件`、`资料`、`工具`、`启用` 五个键；以 `#` 开头的行与空行在元数据区 MUST 被忽略。系统 MUST 在解析阶段校验：文件不超过 8000 个字符；`名称` 必填且不超过 32 个字符、班内唯一；`适用条件` 必填且不超过 120 个字符；`启用` 必填；空行以下 MUST 有非空正文且不超过 2000 个字符；`资料` 不超过 32 个字符。`工具` MUST 接受空值、内置 `knowledge_search` 及 `工具名@服务器名`；MCP 工具名 MUST 符合 `[a-z][a-z0-9_]{0,63}`，服务器名 MUST 符合 `[a-z][a-z0-9_-]{0,31}`。现有中英文逗号、顿号、分号和竖线分隔 MUST 继续支持；重复引用 MUST 按首次出现去重，去重后 MUST 不超过 8 项。未限定服务器的普通工具名、非法字符或额外的 `@` MUST 被拒绝。同一字段重复出现、出现未定义字段、或空行以下缺少正文时 MUST 拒绝提交并返回面向教师的可读错误信息。

#### Scenario: 合法技能文件解析入库

- **WHEN** 教师提交一份字段齐全、正文非空的技能文件
- **THEN** 系统 MUST 解析出名称、适用条件、资料、工具与启用状态并写入 `skills`
- **AND** 空行以下的正文 MUST 作为技能说明保存

#### Scenario: 缺少必填字段被拒绝

- **WHEN** 技能文件缺少 `名称`、`适用条件` 或 `启用`
- **THEN** 系统 MUST 返回 400
- **AND** 错误信息 MUST 能指出缺失的字段

#### Scenario: 重复或未定义字段被拒绝

- **WHEN** 技能文件重复写了同一字段，或出现 `工具` 之外的未定义字段
- **THEN** 系统 MUST 返回 400
- **AND** MUST NOT 写入该技能

#### Scenario: 非法工具名被拒绝

- **WHEN** `工具` 包含 `Formula_Hint@campus-tools`、`formula-hint@campus-tools`、`formula_hint` 或多个 `@`
- **THEN** 系统 MUST 在解析阶段返回 400
- **AND** MUST NOT 写入该技能

#### Scenario: 缺少正文被拒绝

- **WHEN** 技能文件空行以下没有正文
- **THEN** 系统 MUST 返回 400

#### Scenario: 超出长度上限被拒绝

- **WHEN** 文件超过 8000 字符，名称、适用条件、资料、正文超过各自上限，或工具去重后超过 8 项
- **THEN** 系统 MUST 返回 400
- **AND** MUST NOT 写入该技能

#### Scenario: 元数据区忽略注释与空行

- **WHEN** 元数据区含以 `#` 开头的行或空行
- **THEN** 系统 MUST 忽略这些行并正常解析其余字段

#### Scenario: 混合工具与旧文件兼容

- **WHEN** 教师提交仅有 `knowledge_search` 的旧文件，或混合合法 MCP 引用且通过可用性校验的文件
- **THEN** 系统 MUST 接受并保留完整原文
- **AND** 多项引用 MUST 能被完整读取，MUST NOT 因原先短字段截断

## ADDED Requirements

### Requirement: MCP 引用保存前校验

创建和更新技能时，系统 MUST 在写入前验证每个 MCP 引用的服务器属于当前会话班级、已连接、无待确认变更且工具存在于可用发现快照。不存在或他班服务器 MUST 以相同 404 拒绝；未连接、待确认或工具不在清单 MUST 以可读 400 拒绝。任一引用失败 MUST 不写入任何部分技能更新；仅含内置工具或空工具的文件 MUST 不依赖 MCP 可用性。

#### Scenario: 发现后绑定成功

- **WHEN** 教师连接本班自建服务并保存 `工具：knowledge_search,formula_hint@campus-tools`
- **THEN** 系统 MUST 保存两项工具引用和原始文件
- **AND** 返回内容 MUST 不含 MCP 凭据

#### Scenario: 断开或未知工具拒绝保存

- **WHEN** 服务器已断开、等待快照确认，或引用的工具未被发现
- **THEN** 创建和更新 MUST 返回说明失败引用的 400
- **AND** 原有技能与绑定 MUST 保持不变

#### Scenario: 他班引用不泄露

- **WHEN** 教师保存的引用只存在于其他班级或根本不存在
- **THEN** 两种情况 MUST 返回一致的 404
- **AND** MUST NOT 为验证引用而请求他班远端服务器
