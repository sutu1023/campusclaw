# Spec Delta

## MODIFIED Requirements

### Requirement: OpenAPI 描述由处理器注解生成

系统 MUST 由处理器上的注解生成 OpenAPI 描述，并 MUST 通过 `GET /openapi.json` 提供。描述 MUST 覆盖主要接口的路径与方法、路径与查询参数、请求与响应结构、错误码以及是否需要登录；受保护接口 MUST 标注为需要 Bearer 令牌。文档 MUST 与注解同源：修改处理器注解即反映到描述。覆盖范围 MUST 包含 MCP 服务器登记、列表、读取、修改、删除、连接、断开、快照确认、工具目录及审计，并更新技能保存验证和 SSE 多次 `meta.tool_calls` 的说明。文档 MUST 区分应用登录 Bearer 与仅服务端使用的 MCP Bearer；示例和响应结构 MUST NOT 暴露 MCP 令牌。

#### Scenario: 读取 OpenAPI 描述

- **WHEN** 请求 `GET /openapi.json`
- **THEN** 系统 MUST 返回一份可解析的 OpenAPI 描述
- **AND** MUST NOT 要求登录

#### Scenario: 覆盖本课主要接口

- **WHEN** 检查返回的描述
- **THEN** MUST 含智能体、技能、会话与在会话中提问等既有接口，以及全部 MCP 管理、目录、确认与审计接口
- **AND** MUST 含既有登录、检索与材料接口

#### Scenario: 标注错误码与鉴权

- **WHEN** 检查受保护接口的条目
- **THEN** MUST 标注需要应用登录的 Bearer 令牌及教师限制
- **AND** MUST 列出 400 / 401 / 403 / 404 / 409 / 503 中该接口实际可能出现的错误码

#### Scenario: 文档与注解同源

- **WHEN** 修改某处理器注解后重新生成描述
- **THEN** `GET /openapi.json` 的内容 MUST 随之更新

#### Scenario: Swagger 试调 MCP 闭环

- **WHEN** 教师在 `/swagger/index.html` 使用应用登录令牌依次登记、连接、读取工具并保存含 MCP 引用的技能
- **THEN** 请求 MUST 命中实际注册接口并与文档一致
- **AND** 浏览器 MUST 不需要输入或取得真实 MCP 令牌
