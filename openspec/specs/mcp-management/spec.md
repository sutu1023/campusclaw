# mcp-management Specification

## Purpose

让教师在本班登记并管理部署方批准的 MCP 服务，看到经过握手发现的工具说明和输入结构，并以明确的连接、断开及快照确认控制调用权限；保证班级归属和服务端凭据不会因工具接入而泄漏。

## Requirements

### Requirement: 教师管理与班级归属

系统 MUST 提供 `/api/mcp/servers` 的列表和登记，以及 `/api/mcp/servers/{id}` 的读取、更新和删除；名称 MUST 符合 `[a-z][a-z0-9_-]{0,31}`、班内唯一且创建后不可改名，删除后 MUST NOT 将该名称重新分配给另一服务器。所有 MCP 管理、工具目录和审计接口 MUST 要求有效登录及 teacher 角色：未登录 401，学生 403；按编号访问他班或不存在的资源 MUST 返回相同的 404。班级 MUST 只来自服务端会话，MUST NOT 采信请求中的 `class_id`。

#### Scenario: 教师登记与持久化

- **WHEN** 教师登记合法服务器并在 API 重启后列出服务器
- **THEN** 服务器 MUST 仍属于其会话班级并可被列出
- **AND** 同班重名登记 MUST 返回 400，其他班的独立登记 MUST 不受影响

#### Scenario: 拒绝非教师管理与跨班探测

- **WHEN** 学生调用任意 MCP 管理接口，或教师按编号访问他班服务器
- **THEN** 系统 MUST 分别返回 403 或与不存在一致的 404
- **AND** MUST NOT 产生网络调用或数据变更

### Requirement: 环境凭据与目标地址绑定

MCP 凭据原文 MUST 仅从 CampusClaw 服务端环境读取，登记数据 MUST 仅保存凭据引用。凭据使用 MUST 同时匹配部署方批准的班级、服务器名称和规范化 Base URL；教师 MUST NOT 借登记将其他班级的凭据发送至其他地址。Base URL MUST 限于部署方显式批准的 HTTP(S) origin，MUST NOT 包含用户信息、查询、片段或非根路径；出站请求 MUST NOT 跟随重定向。响应 MUST 只以 `has_token` 表示是否具备匹配凭据，MUST NOT 包含令牌原文或任何可还原密钥的字段。

#### Scenario: 凭据不回传

- **WHEN** 教师登记、读取、修改、连接或列出服务器
- **THEN** 响应 MUST 不含真实 MCP 令牌
- **AND** `has_token` MUST 反映当前服务端环境中匹配凭据是否可用

#### Scenario: 拒绝凭据外发与地址绕过

- **WHEN** 登记未经批准的地址、带用户信息的 URL，或尝试把批准的凭据绑定到其他班级/地址
- **THEN** 系统 MUST 返回可读的 400
- **AND** MUST NOT 向该地址发送带令牌请求

### Requirement: 握手发现与原子快照

教师 MUST 能通过 `POST /api/mcp/servers/{id}/connect` 触发初始化、完成通知和 `tools/list`，首次发现或无需重新确认的发现成功后，系统 MUST 将本次有效工具清单整体替换为本地快照并标记为已连接；需要确认的变更 MUST 按下述变更确认要求保持不可调用。`GET /api/mcp/servers/{id}/tools` MUST 返回工具名、完整说明和输入结构。发现 MUST 拒绝重名、非法工具名、不可安全校验的输入结构及超限清单；任一步失败 MUST 保持不可调用、清空可用快照并返回脱敏的可读错误，MUST NOT 使用半份清单或旧清单继续调用。

#### Scenario: 重连不重复且移除旧工具

- **WHEN** 连续连接两次且第二次远端移除了某工具
- **THEN** 本地工具条数 MUST 不因重复连接增加
- **AND** 被移除的工具 MUST 不再可发现、绑定或调用

#### Scenario: 握手或发现失败

- **WHEN** 地址不可达、令牌错误、版本不兼容或清单无效
- **THEN** 连接 MUST 失败且错误可读、无密钥
- **AND** 服务器 MUST 不可调用，既有检索和对话服务 MUST 仍可用

### Requirement: 工具描述变更确认

首次成功发现的自建服务快照 MUST 可用于连接。后续发现中已有工具说明或输入结构发生改变时，系统 MUST 记录前后快照标识与时间，暂停该服务器调用，并展示完整待确认内容；只有本班教师通过 `POST /api/mcp/servers/{id}/approve` 确认当前快照标识后，系统 MUST 才能启用新快照。确认过期标识 MUST 返回 409，断开 MUST 取消待确认内容。

#### Scenario: 变更先确认再使用

- **WHEN** 已连接服务重连时工具说明被改写
- **THEN** 系统 MUST 标为待确认并展示变化
- **AND** 确认前的保存和调用 MUST 被拒绝，确认匹配快照后 MUST 才恢复可用

### Requirement: 断开更新删除即失效

教师 MUST 能通过 `POST /api/mcp/servers/{id}/disconnect` 断开服务器。断开、修改地址/凭据绑定或删除 MUST 使连接失效并清空可用及待确认清单；既有技能文本 MUST 保留但后续调用 MUST 返回 `denied`，MUST NOT 隐式重连。已在断开前获准发出的只读请求不要求撤回，但断开后新授权的调用 MUST 被拒绝。删除 MUST 保留历史调用的来源快照，MUST NOT 删除审计证据。

#### Scenario: 断开后重复提问

- **WHEN** 教师断开服务器，学生再次用原技能提问
- **THEN** 目录 MUST 为空，本次该引用 MUST 显示 `denied`
- **AND** MUST NOT 新发远端工具调用，最终问答 MUST 仍能继续

#### Scenario: 删除后保留历史

- **WHEN** 教师删除有历史调用的服务器
- **THEN** 服务器 MUST 不再可连接或调用
- **AND** 历史记录 MUST 仍保留当时的服务器名和工具引用
