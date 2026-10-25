# mcp-server Specification

## Purpose

在 CampusClaw 同一仓库提供可独立验证的真实教学工具服务，使宿主通过标准化的初始化、发现和调用报文取得有边界的文本提示，并能够验证网络互通、令牌鉴权与协议错误，而非只运行模拟逻辑。

## Requirements

### Requirement: 同仓服务与部署隔离

系统 MUST 在同一 Git 仓库的 `mcp-server/` 提供独立服务，并由根 `docker-compose.yml` 构建和启动。API MUST 能通过 Compose 内网 Base URL `http://mcp-demo:8090` 访问它；浏览器 MUST NOT 直接访问 MCP 端点，服务 MUST NOT 默认映射宿主端口。`GET /health` MUST 无需认证返回 200 和可解析的健康状态。

#### Scenario: Compose 真实互通

- **WHEN** 使用已配置环境变量的 Compose 部署并从 API 容器访问 MCP 服务
- **THEN** `/health` MUST 返回健康状态
- **AND** 通过该内网地址的初始化、工具发现和调用 MUST 实际到达 `mcp-server` 进程

### Requirement: Bearer 鉴权与密钥边界

`POST /mcp` MUST 要求与服务端环境变量 `MCP_AUTH_TOKEN` 一致的 Bearer 令牌；缺失、格式不正确或令牌错误 MUST 返回 401。令牌为空时服务 MUST 拒绝启动。令牌 MUST NOT 硬编码、写入 Git、构建参数、镜像层、前端静态资源、响应正文或日志。

#### Scenario: 缺失或错误令牌

- **WHEN** 未带令牌或携带错误令牌调用任意 MCP 方法
- **THEN** 服务 MUST 返回 401
- **AND** MUST NOT 执行工具或在错误中回显请求头

#### Scenario: 未配置令牌

- **WHEN** 服务启动时未配置 `MCP_AUTH_TOKEN`
- **THEN** 服务 MUST 拒绝以无鉴权模式运行

### Requirement: JSON-RPC 工具协议

`POST /mcp` MUST 支持 JSON-RPC 2.0 的 `initialize`、`tools/list`、`tools/call`，请求和响应 MUST 通过相同 `id` 关联。初始化 MUST 协商支持的协议版本并声明工具能力；不兼容版本 MUST 明确失败。服务 MUST 接受初始化完成通知且不生成伪造响应。工具发现 MUST 为每个工具提供唯一名称、完整说明及对象类型的 `inputSchema`；调用 MUST 返回文本 `content` 数组和 `isError`。已鉴权的非法 JSON、无效请求、未知方法及非法方法参数 MUST 分别给出可读的 JSON-RPC 错误，MUST NOT 暴露栈或密钥。

#### Scenario: 初始化与发现

- **WHEN** 宿主携带有效令牌发送兼容的初始化、完成通知和工具发现请求
- **THEN** 服务 MUST 声明工具能力并返回至少两个有效工具定义
- **AND** 请求响应的 `id` MUST 正确对应

#### Scenario: 非法协议请求

- **WHEN** 已鉴权请求含非法 JSON、无效 JSON-RPC 结构、未知方法或非法参数
- **THEN** 服务 MUST 返回对应的 JSON-RPC 错误代码和可读消息
- **AND** MUST NOT 把错误伪装成成功的工具文本

### Requirement: 两个只读教学提示工具

服务 MUST 暴露 `formula_hint` 和 `unit_convert`；工具名 MUST 符合 `[a-z][a-z0-9_]{0,63}`。两者 MUST 接收 `query` 非空字符串并校验上限 500 个字符；MUST 返回非空文本，MUST NOT 写文件、调用外部网络或产生持久化副作用。`formula_hint` MUST 提供公式适用条件或下一步检查提示；`unit_convert` MUST 提供单位换算的关系、量纲及步骤提示，而不是给出具体练习的最终数值答案。

#### Scenario: 公式提示

- **WHEN** 调用 `formula_hint` 并询问去括号或勾股定理的使用
- **THEN** 工具 MUST 返回相关概念或下一步检查提示
- **AND** MUST NOT 直接给出该练习的最终答案

#### Scenario: 单位换算提示

- **WHEN** 调用 `unit_convert` 并询问长度、质量或时间换算
- **THEN** 工具 MUST 返回适用的单位关系和换算方向提示
- **AND** MUST NOT 替学生完成具体数值计算
