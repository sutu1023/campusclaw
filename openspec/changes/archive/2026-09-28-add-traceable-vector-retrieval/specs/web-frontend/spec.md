# Spec Delta

## Purpose

CampusClaw 前端为独立的单页应用（SPA），由 Nginx 托管并经 `/api` 调用后端；所有受保护请求以 Bearer token 认证，界面统一采用北大红主题，替代此前的服务端渲染模板。

## ADDED Requirements

### Requirement: 独立 SPA 前端

系统 MUST 提供独立的单页应用（SPA）作为用户界面，MUST NOT 依赖服务端模板渲染业务页面；SPA 构建产物 MUST 由 Nginx 在浏览器入口托管，并对前端路由提供 history fallback；对 `/api/*` 以外的未知前端路径 MUST 返回 SPA 入口而非 404（受保护资源仍由后端鉴权）。

#### Scenario: SPA 由 Nginx 托管

- **WHEN** 浏览器访问应用根路径或前端路由（如 `/materials`、`/search`）
- **THEN** Nginx MUST 返回 SPA 入口页面
- **AND** 深层前端路由刷新 MUST NOT 返回 404

#### Scenario: 业务页面不再由服务端渲染

- **WHEN** 请求业务页面路径
- **THEN** 响应 MUST 为 SPA 入口与静态资源
- **AND** 页面内容 MUST 由 SPA 在浏览器端调用 `/api/*` 渲染
- **AND** MUST NOT 由 Go 服务渲染业务 HTML 模板

### Requirement: Token 方式的 API 通信

SPA MUST 在登录成功后保存服务端签发的不透明令牌，并在所有受保护 API 请求中以 `Authorization: Bearer <token>` 头携带；令牌 MUST NOT 出现在 URL、查询串、hash 或日志中；收到 401 时 SPA MUST 清除本地令牌并跳转到登录页。

#### Scenario: 受保护请求携带 Bearer 头

- **WHEN** 已登录用户通过 SPA 访问材料、检索或问答接口
- **THEN** 每个请求 MUST 带 `Authorization: Bearer <token>`
- **AND** 服务端 MUST 据此解析用户与班级

#### Scenario: 401 时跳转登录

- **WHEN** 任一 API 返回 401（令牌缺失、无效或过期）
- **THEN** SPA MUST 清除本地保存的令牌
- **AND** MUST 跳转到登录页
- **AND** MUST NOT 继续展示受保护数据

#### Scenario: 令牌不进入 URL

- **WHEN** SPA 构造请求或前端路由
- **THEN** 令牌 MUST NOT 出现在 URL、查询参数或 hash 中

### Requirement: 北大红视觉主题

SPA MUST 统一采用北大红视觉主题，主色 MUST 为 `#94070A`，辅助色 MUST 为 `#BE2126`；主题 MUST 覆盖登录、材料列表 / 上传 / 详情、检索与问答的主要界面元素（按钮、链接、强调、标题等）；正文与背景的对比度 MUST 满足可读性要求。

#### Scenario: 主题色应用于主要界面

- **WHEN** 渲染登录、材料、检索与问答界面
- **THEN** 主操作按钮与强调元素 MUST 使用主色 `#94070A`
- **AND** 辅助强调 MUST 使用辅助色 `#BE2126`

#### Scenario: 可读性

- **WHEN** 在北大红主题下渲染正文与背景
- **THEN** 文本与背景的对比度 MUST 满足 WCAG AA（普通文本 ≥ 4.5:1）

### Requirement: 前端页面范围与角色渲染

SPA MUST 至少覆盖登录、本班材料列表与上传、材料详情、检索与问答界面，并 MUST 按服务端返回的角色与班级渲染（教师可见上传入口，学生仅只读）；前端隐藏入口 MUST NOT 被视为权限控制手段，服务端 MUST 独立校验。

#### Scenario: 按角色渲染

- **WHEN** 教师登录
- **THEN** SPA MUST 展示材料上传入口
- **AND** WHEN 学生登录
- **THEN** SPA MUST NOT 展示上传入口
- **AND** 服务端仍 MUST 拒绝学生的写请求
