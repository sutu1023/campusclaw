# Spec Delta

## Purpose

让对话网关与嵌入网关复用同一 API，使演示环境只需配置一套网关地址与密钥即可同时支持向量化与问答。

## ADDED Requirements

### Requirement: 对话与嵌入共用网关 API

系统 MUST 在未显式提供对话网关地址或密钥时，回退使用嵌入网关的地址与密钥；对话模型 MUST 由独立配置项指定（默认 `course-chat`）。网关地址与密钥 MUST 仅由服务端读取，MUST NOT 下发浏览器。

#### Scenario: 未配置对话网关时回退

- **WHEN** 仅提供嵌入网关的地址与密钥，未提供对话网关的地址与密钥
- **THEN** 系统 MUST 使用嵌入网关的地址与密钥调用对话接口
- **AND** 在存在依据时问答 MUST 能返回回答

#### Scenario: 显式配置优先

- **WHEN** 显式提供对话网关地址或密钥
- **THEN** 系统 MUST 使用显式配置，而不是回退值

#### Scenario: 对话模型可配置

- **WHEN** 提供对话模型配置项
- **THEN** 调用对话接口时 MUST 使用该模型名
