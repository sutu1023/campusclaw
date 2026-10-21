# Spec Delta

## Purpose

让教师在本班维护「智能体」这一配置单位：把角色提示词与「能用的技能」「能查的材料」绑在一起；学生只能选用本班已启用的智能体，一次回答的角色设定与可用范围都以它为准。

## ADDED Requirements

### Requirement: 智能体数据模型与班内名称唯一

系统 MUST 以 `tutor_agents` 保存智能体，字段包含 `id`、`class_id`、`name`、`prompt`、`enabled`；`name` MUST 在同一班级内唯一且不超过 64 个字符；`prompt` MUST 不超过 2000 个字符，为空时 MUST 回退到默认角色设定而不是拒绝请求；`enabled` 表示启用状态。智能体 MUST NOT 复制技能与材料的正文，只通过关联表保存绑定关系。

#### Scenario: 新建智能体成功

- **WHEN** 教师在本班提交合法的 `name` 与 `prompt`
- **THEN** 系统 MUST 新增一条归属于该教师班级的智能体记录
- **AND** 响应 MUST 返回其 `id`、`class_id`、`name`、`prompt` 与 `enabled`

#### Scenario: 名称超过长度上限被拒绝

- **WHEN** 提交的 `name` 超过 64 个字符
- **THEN** 系统 MUST 返回 400
- **AND** MUST NOT 新增智能体记录

#### Scenario: 班内同名被拒绝

- **WHEN** 在同一班级内提交一个已存在的 `name`
- **THEN** 系统 MUST 返回 400
- **AND** MUST NOT 新增或覆盖既有记录

#### Scenario: 跨班同名允许

- **WHEN** 班级 B 提交的 `name` 与班级 A 某智能体相同
- **THEN** 系统 MUST 允许创建
- **AND** 两个班级的记录 MUST 互不影响

### Requirement: 教师管理智能体

系统 MUST 允许教师创建、修改与启停本班智能体，并 MUST 在修改时同时提交所绑定的技能编号与材料编号；学生调用创建或修改接口 MUST 在写入数据之前被拒绝（403）。角色 MUST 以服务端会话中的用户记录为准，MUST NOT 采信请求中自行声明的角色。

#### Scenario: 学生写入被拒绝

- **WHEN** 以 student 角色的有效会话调用智能体创建或修改接口
- **THEN** 系统 MUST 返回 403
- **AND** `tutor_agents` 与绑定表 MUST 无新增或变更

#### Scenario: 教师创建并绑定

- **WHEN** 教师提交智能体及其 `skill_ids` 与 `material_ids`
- **THEN** 系统 MUST 写入智能体并建立对应绑定关系
- **AND** 读取该智能体时 MUST 返回相同的 `skill_ids` 与 `material_ids`

#### Scenario: 教师修改绑定后即时生效

- **WHEN** 教师为一个已有智能体解绑某技能或材料
- **THEN** 该智能体的绑定关系 MUST 在下一次读取时即为新关系
- **AND** MUST NOT 需要重建智能体或新建会话即可生效

### Requirement: 角色提示词回退

系统 MUST 接受 `prompt` 为空的智能体；当使用该智能体的会话提问时，系统 MUST 以默认角色设定继续作答，MUST NOT 因提示词为空而拒绝请求或返回错误。

#### Scenario: 空提示词仍可创建并作答

- **WHEN** 教师创建一个 `prompt` 为空的智能体，学生新建会话并向其提问
- **THEN** 系统 MUST 正常受理并生成回答
- **AND** MUST NOT 因 `prompt` 为空返回 400 或 404

### Requirement: 技能与材料绑定

系统 MUST 只允许把本班技能与本班材料绑定到本班智能体；绑定他班或不存在的技能、材料 MUST 按「不存在」处理并返回 404，MUST NOT 建立该绑定。空绑定 MUST 是合法状态：未绑定技能时该智能体不参与任何技能路由，未绑定材料时该次回答的检索范围为空。未绑定或已停用的技能 MUST NOT 参与路由，即使其适用条件命中提问。

#### Scenario: 只能绑定本班资源

- **WHEN** 教师把本班智能体绑定到本班某个技能或材料
- **THEN** 系统 MUST 建立该绑定关系

#### Scenario: 绑定他班资源按不存在处理

- **WHEN** 请求把本班智能体绑定到他班的技能或材料编号
- **THEN** 系统 MUST 返回 404
- **AND** MUST NOT 建立跨班绑定

#### Scenario: 未绑定技能不参与路由

- **WHEN** 某技能未被该智能体绑定，但其适用条件命中提问
- **THEN** 该技能 MUST NOT 进入该智能体的候选集
- **AND** 回答 MUST NOT 采用该技能的引导正文

#### Scenario: 空材料绑定检索为空

- **WHEN** 智能体未绑定任何材料，学生用它提问
- **THEN** 系统 MUST 照常作答
- **AND** 引用列表 MUST 为空

### Requirement: 智能体班级隔离与启用过滤

系统 MUST 在服务端按会话班级过滤智能体；学生列出本班智能体时 MUST 只得到已启用的记录。按编号访问未启用、他班或不存在的智能体时，系统 MUST 返回完全一致的 404，使调用方无法区分三者的差异。

#### Scenario: 学生列表只见已启用

- **WHEN** 学生列出本班智能体，本班存在已启用与未启用的记录
- **THEN** 返回集合 MUST 只含已启用的智能体
- **AND** MUST NOT 出现未启用智能体的名称或提示词

#### Scenario: 停用后的智能体对读取表现为不存在

- **WHEN** 以学生身份按编号读取一个已停用的本班智能体
- **THEN** 系统 MUST 返回 404
- **AND** 响应 MUST 与读取一个不存在编号时完全一致

#### Scenario: 跨班读取表现为不存在

- **WHEN** 以班级 A 的会话按编号读取班级 B 的智能体
- **THEN** 系统 MUST 返回 404
- **AND** MUST NOT 返回该智能体的名称、提示词或绑定信息
