# demo-knowledge-base Specification

## Purpose

为检索与问答提供一份内置、可复现的演示知识库与示例问句，使课堂演示、逐条验收与新用户上手在没有外部数据准备时即可运行。

## Requirements

### Requirement: 预置演示知识库

系统 MUST 在初始化/种子阶段预置一组演示材料，班级 A 与班级 B MUST 各自至少包含两篇主题明确、正文充实的材料；每篇演示材料 MUST 保存上传原文（`knowledge_entries.body_text`），并 MUST 触发切片与向量入库流程。种子执行 MUST 幂等：重复初始化 MUST NOT 产生重复材料或重复正文。

#### Scenario: 双班演示材料存在且可区分

- **WHEN** 首次完成初始化/种子（含 Compose 首次启动）
- **THEN** 班级 A MUST 存在至少两篇演示材料
- **AND** 班级 B MUST 存在至少两篇演示材料
- **AND** 每篇演示材料 MUST 具备非空正文
- **AND** 至少一篇 A 班材料标题含「A 班」、至少一篇 B 班材料标题含「B 班」

#### Scenario: 种子幂等

- **WHEN** 在已有数据的库上重复执行种子
- **THEN** 演示材料与正文条数 MUST NOT 增加重复记录

#### Scenario: 演示材料触发入库

- **WHEN** 演示材料完成种子
- **THEN** 系统 MUST 为其生成切片并写入索引状态
- **AND** 若嵌入网关可用，切片 MUST 完成向量写入并置为 ready
- **AND** 若嵌入网关不可用，切片 MUST 保留为 pending/failed，并可由补嵌或重建补齐

### Requirement: 演示语料可检索与可问答

演示语料 MUST 支持关键字、向量与混合三种检索模式，并可被问答引用：关键字模式 MUST 能由演示正文中的词命中；向量模式 MUST 能由语义相近的问句命中；问答 MUST 返回带 `[n]` 出处的回答。

#### Scenario: 关键字命中

- **WHEN** 在本班以演示正文中出现的关键词执行 keyword 检索
- **THEN** MUST 返回至少一条命中
- **AND** 命中 MUST 含材料标题、切片序号与字符区间

#### Scenario: 向量命中

- **WHEN** 以演示内容的同义问句执行 vector 检索
- **THEN** MUST 返回至少一条命中

#### Scenario: 问答带出处

- **WHEN** 以演示语料可回答的问题调用问答接口
- **THEN** MUST 返回回答且 `citations` MUST 非空
- **AND** 回答中的 `[n]` MUST 与 `citations` 同序

### Requirement: 演示数据的班级隔离

演示语料 MUST 包含仅属于本班的专属主题或词；跨班检索 MUST 对外表现为无命中，MUST NOT 返回他班演示材料。

#### Scenario: 跨班专属词无命中

- **WHEN** A 班会话检索仅出现在 B 班演示语料中的专属词
- **THEN** 命中列表 MUST 为空
- **AND** MUST NOT 返回 B 班演示材料的标题或正文

### Requirement: 示例问句

系统 MUST 经服务端提供一组可由演示语料回答的示例问句，并 MUST 至少包含一条无依据问句（预期「资料中未找到相关内容」）；示例问句 MUST 在检索与问答界面展示，用户点击某条示例问句 MUST 以该问句发起对应查询。

#### Scenario: 展示并一键使用示例问句

- **WHEN** 已登录用户打开检索或问答界面
- **THEN** 界面 MUST 展示服务端提供的示例问句
- **AND** 点击某条示例问句 MUST 以该问句发起对应查询

#### Scenario: 无依据示例问句

- **WHEN** 用户使用标记为无依据的示例问句发起问答
- **THEN** 系统 MUST NOT 调用对话网关
- **AND** MUST 返回固定文案且 `citations` 为空

### Requirement: 可复现的演示验收

系统 MUST 在文档中记录一组样例查询及其预期结果（检索模式、预期命中材料、是否有出处），使演示场景可逐条复现。

#### Scenario: 文档样例可复现

- **WHEN** 操作者按文档执行样例查询
- **THEN** 实际结果 MUST 与文档记录的预期一致
