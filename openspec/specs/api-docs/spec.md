# api-docs Specification

## Purpose

让接口契约与实现同源可查：由处理器注解生成 OpenAPI 描述并提供可试调的 Swagger 页面，使文档与注册路径、入参出参、错误码和鉴权要求保持一致。

## Requirements

### Requirement: OpenAPI 描述由处理器注解生成

系统 MUST 由处理器上的注解生成 OpenAPI 描述，并 MUST 通过 `GET /openapi.json` 提供。描述 MUST 覆盖主要接口的路径与方法、路径与查询参数、请求与响应结构、错误码以及是否需要登录；受保护接口 MUST 标注为需要 Bearer 令牌。文档 MUST 与注解同源：修改处理器注解即反映到描述。

#### Scenario: 读取 OpenAPI 描述

- **WHEN** 请求 `GET /openapi.json`
- **THEN** 系统 MUST 返回一份可解析的 OpenAPI 描述
- **AND** MUST NOT 要求登录

#### Scenario: 覆盖本课主要接口

- **WHEN** 检查返回的描述
- **THEN** MUST 含智能体、技能、会话与在会话中提问等本课新增接口
- **AND** MUST 含既有登录、检索与材料接口

#### Scenario: 标注错误码与鉴权

- **WHEN** 检查受保护接口的条目
- **THEN** MUST 标注需要 Bearer 令牌
- **AND** MUST 列出 400 / 401 / 403 / 404 / 503 中该接口实际可能出现的错误码

#### Scenario: 文档与注解同源

- **WHEN** 修改某处理器注解后重新生成描述
- **THEN** `GET /openapi.json` 的内容 MUST 随之更新

### Requirement: Swagger 页面可对照试调

系统 MUST 提供 `GET /swagger/index.html` 返回 Swagger UI 页面，MUST 加载上述 OpenAPI 描述。文档路由 MUST 与业务路由共存且不被覆盖；处理器注解的路径 MUST 与实际注册路径一致，否则该接口 MUST 视为未文档化。

#### Scenario: 打开 Swagger 页面

- **WHEN** 浏览器访问 `GET /swagger/index.html`
- **THEN** 系统 MUST 返回可加载 OpenAPI 描述的 HTML 页面

#### Scenario: 按文档试调主要接口

- **WHEN** 在 Swagger 页面上对主要接口填参并发起请求
- **THEN** 请求 MUST 命中实际接口
- **AND** 返回 MUST 与该接口行为一致

#### Scenario: 注解路径与注册路径不一致时接口缺失

- **WHEN** 某处理器的注解路径与实际注册路径不一致
- **THEN** 该接口 MUST NOT 出现在可试调的接口清单中
