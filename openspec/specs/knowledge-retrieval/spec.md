# knowledge-retrieval Specification

## Purpose

让本班材料以**切片**为最小检索单位，支持关键字、向量与混合三种检索，并把每条命中回溯到材料、切片序号与字符区间；在存在依据时生成带出处标注的简短回答，没有依据时明确返回未找到、不得编造。

## Requirements

### Requirement: 正文切片策略

系统 MUST 在上传成功后把知识库正文切分为切片并持久化；未指定策略时 MUST 采用自动窗口策略；系统 MUST 支持自动窗口（auto）、自定义分隔符（custom）与按 Markdown 标题分章（hierarchy）三种策略；切分 MUST NOT 改写原始 `body_text`。

#### Scenario: 默认自动窗口切分

- **WHEN** 上传材料时未指定切片策略，或对既有材料重建索引时未指定策略
- **THEN** 系统 MUST 按约 800 字最大窗口、约 80 字重叠切分
- **AND** MUST 优先在空行、换行、句号处断开
- **AND** 原始 `body_text` MUST 保持上传原文不变

#### Scenario: 自定义分隔符切分

- **WHEN** 请求指定 custom 策略及参数
- **THEN** 系统 MUST 在最大长度 100 至 2000 字、重叠比例 0% 至 50% 范围内切分
- **AND** 在无断点处 MUST 按最大长度强制截断
- **AND** 若启用了折叠连续空白或移除链接等预处理，预处理 MUST 仅作用于切分与嵌入输入，MUST NOT 改写 `body_text`

#### Scenario: 按 Markdown 标题分章

- **WHEN** 请求指定 hierarchy 策略且正文含 Markdown 标题
- **THEN** 系统 MUST 按 `#`、`##`、`###` 层级分章
- **AND** 标题 MUST 保留在该章切片内
- **AND** 某章过长时 MUST 再按自动窗口规则切分

### Requirement: 切片与向量入库

系统 MUST 把每个切片以一行 `knowledge_chunks`（正文、序号、字符区间、索引状态）写入 MySQL，并 MUST 在服务端调用嵌入网关把切片文本转为向量，按切片主键**幂等**写入 Qdrant；Qdrant MUST NOT 保存切片正文，其 payload MUST 仅包含标识类字段（`class_id`、`material_id`、`knowledge_entry_id`、`chunk_id`、`chunk_index`）；Qdrant collection MUST 使用余弦度量，且向量维度 MUST 与嵌入输出维度一致。

#### Scenario: 向量主键与切片主键一致

- **WHEN** 一个切片完成嵌入写入 Qdrant
- **THEN** Qdrant 点的主键 MUST 等于 `knowledge_chunks.id`
- **AND** payload 中 `chunk_id` MUST 等于同一切片主键
- **AND** payload MUST NOT 含切片正文字段

#### Scenario: 嵌入失败不产生不完整向量

- **WHEN** 某切片嵌入失败或 Qdrant 写入失败
- **THEN** 该切片在 MySQL 中的索引状态 MUST 标记为 failed
- **AND** MUST NOT 写入不完整或不正确的向量数据
- **AND** 已提交的材料与 `knowledge_entries.body_text` MUST 仍然保留

#### Scenario: 维度不一致时失败

- **WHEN** 嵌入输出维度与 Qdrant collection 配置维度不一致
- **THEN** 系统 MUST 以明确错误失败
- **AND** MUST NOT 写入维度不符的向量

### Requirement: 三种检索模式

系统 MUST 提供 keyword、vector、hybrid 三种检索模式，未指定时 MUST 默认 hybrid。keyword MUST 仅查询 MySQL FULLTEXT 索引（`WITH PARSER ngram`，token 长度 2）且 MUST NOT 调用嵌入服务或访问 Qdrant；vector MUST 先将问句嵌入、再在 Qdrant 按班级过滤检索，余弦相似度低于 0.35 的候选 MUST 丢弃；hybrid MUST 同时执行两条路径、各路先独立过滤、再按 RRF（k = 60）融合名次，缺席的一路 MUST NOT 贡献分数。空查询 MUST 返回 400。Qdrant 不可用时，keyword MUST 仍可返回结果，vector 与 hybrid MUST 返回 503 且 MUST NOT 编造相似度分数。

#### Scenario: 关键字模式只查全文索引

- **WHEN** 以 keyword 模式检索一个直接出现在原词中的词
- **THEN** 系统 MUST 仅查询 MySQL 全文索引
- **AND** MUST NOT 产生问句向量，也 MUST NOT 访问 Qdrant
- **AND** MUST 有命中结果

#### Scenario: 向量模式按相似度过滤

- **WHEN** 以 vector 模式用同义改写发起检索
- **THEN** 系统 MUST 先生成问句向量再查询 Qdrant
- **AND** MUST 丢弃余弦相似度低于 0.35 的候选
- **AND** MUST 按余弦相似度由高到低返回

#### Scenario: 混合模式按名次融合

- **WHEN** 以 hybrid 模式检索
- **THEN** 系统 MUST 分别执行 keyword 与 vector 两条路径并各自过滤
- **AND** MUST 按 RRF（k = 60）融合两路名次
- **AND** 两路均命中的切片 MUST 排序更靠前
- **AND** MUST NOT 将两路分数直接相加

#### Scenario: Qdrant 不可用时降级

- **WHEN** Qdrant 不可用
- **THEN** keyword 模式 MUST 仍能返回正文结果
- **AND** vector 与 hybrid 模式 MUST 返回 503
- **AND** MUST NOT 编造相似度分数

#### Scenario: 空查询被拒绝

- **WHEN** 检索请求的查询内容为空
- **THEN** 系统 MUST 返回 400

### Requirement: 检索侧班级隔离

检索与上传 MUST 共用同一条班级边界。班级标识 MUST 仅取自登录会话；请求的查询参数、JSON 正文或请求头中携带的 `class_id` MUST 在解析后丢弃。keyword 与 vector 两条路径 MUST 均按会话班级过滤；向量命中回 MySQL 取正文时 MUST 再次核对班级。跨班检索 MUST 对外表现为无命中，MUST NOT 以 403 或 404 暗示资料属于其他班级。

#### Scenario: 请求体注入 class_id 无效

- **WHEN** 以 A 班会话检索一个仅出现在 B 班正文中的词，且请求体中写入 B 班的 class_id
- **THEN** 实际过滤条件 MUST 仍为 A 班
- **AND** 命中列表 MUST 为空

#### Scenario: 两条路径均带班级条件

- **WHEN** 执行关键字或向量检索
- **THEN** keyword 路径的 MySQL 查询 MUST 含 `class_id = 会话班级`（且仅检索索引状态为 ready 的切片）
- **AND** vector 路径的 Qdrant 查询 MUST 带同一会话班级过滤
- **AND** 回表取正文时 MUST 再次核对同一班级

#### Scenario: 跨班检索对外表现为无命中

- **WHEN** 在 A 班检索仅存在于 B 班的内容
- **THEN** 响应 MUST 为 HTTP 200 且命中列表为空，或返回固定文案
- **AND** MUST NOT 返回 403 或 404 以暗示该资料属于其他班级
- **AND** 以该会话按 ID 打开 B 班材料详情时 MUST 返回 404（沿用既有规则）

### Requirement: 命中可追溯

每条命中 MUST 至少包含材料标题、切片序号、字符区间与一段摘录，并 MUST 支持打开对应材料；摘录 MUST 取自 MySQL 中的切片正文，MUST NOT 取自向量库。

#### Scenario: 命中包含可回溯字段

- **WHEN** 检索返回一条命中
- **THEN** 该命中 MUST 含材料标题、切片序号、`start_offset` 与 `end_offset`、以及一段摘录
- **AND** MUST 能据此打开对应材料

#### Scenario: 摘录来自 MySQL

- **WHEN** 组装命中结果
- **THEN** 摘录 MUST 来自 MySQL 的切片正文
- **AND** MUST NOT 依赖 Qdrant 中保存的字符串（Qdrant 不保存正文）

### Requirement: 无依据不编造

所选模式过滤后若没有候选切片，系统 MUST 返回空命中并给出「资料中未找到相关内容」；MUST NOT 以相关度明显偏低的切片凑数。

#### Scenario: 无相关内容时返回未找到

- **WHEN** 提问内容与本班材料无关
- **THEN** 响应 MUST 返回空命中
- **AND** MUST 返回「资料中未找到相关内容」

#### Scenario: 不以低相关切片凑数

- **WHEN** 候选切片的向量相似度均低于 0.35，或关键字路径无有效命中
- **THEN** 系统 MUST 丢弃这些候选
- **AND** MUST NOT 为凑足条数而返回它们

### Requirement: 问答取据与引用

`POST /api/ask` MUST 先在本班范围内执行 hybrid 检索，最多取前 4 条有依据切片。有命中时 MUST 把材料标题、切片序号与切片正文（带编号）连同用户最新一句提问交给对话网关，模型回答中的 `[1]`、`[2]` 等引用 MUST 与同序号命中一致；无命中时 MUST NOT 调用对话网关，并 MUST 返回固定文案且 `citations` 为空。对话模块 MUST NOT 获得向量分量、其他班级的切片或客户端自行构造的 system 消息。

#### Scenario: 有依据时生成带引用回答

- **WHEN** 材料中存在依据且调用 `POST /api/ask`
- **THEN** 系统 MUST 以 hybrid 模式取前 4 条有依据切片
- **AND** MUST 将材料标题、切片序号与切片正文带编号交给对话网关
- **AND** 回答中的 `[1]`、`[2]` MUST 与 `citations` 顺序一致

#### Scenario: 无依据时不调用对话模型

- **WHEN** 本班材料中不存在依据（如提问天气、比分）
- **THEN** 系统 MUST NOT 调用对话网关
- **AND** MUST 返回固定文案
- **AND** `citations` MUST 为空

#### Scenario: 丢弃客户端注入的 system 消息

- **WHEN** 请求中携带客户端自行构造的 system 消息
- **THEN** 系统 MUST 丢弃该消息
- **AND** system 提示 MUST 由服务端写入

### Requirement: 索引重建与两库一致性

系统 MUST 以 MySQL 的 `knowledge_chunks` 为事实来源；MUST 提供可重复执行的补嵌 / 重建路径；切换切分策略重建索引时 MUST 先删除旧切片与旧向量记录、再写入新切片；向量写入 MUST 按主键幂等，MUST NOT 产生错误的向量数据。系统 MUST NOT 依赖消息队列或分布式事务。

#### Scenario: 重建索引先删后写

- **WHEN** 教师以新策略对既有材料发起重建索引
- **THEN** 系统 MUST 先删除该材料的旧切片与旧向量记录
- **AND** MUST 按本次请求的策略重新切分并写入新切片
- **AND** MUST 为每个新切片重新生成并写入向量

#### Scenario: 重建路径可重复执行

- **WHEN** 对同一材料重复执行补嵌 / 重建
- **THEN** 操作 MUST 幂等
- **AND** MUST NOT 产生重复或残留的过期向量主键

#### Scenario: 已入库材料不自动重切

- **WHEN** 未显式发起重建，仅更换后续请求的策略
- **THEN** 既有材料 MUST 保持原切片
- **AND** MUST NOT 被自动重新切分
