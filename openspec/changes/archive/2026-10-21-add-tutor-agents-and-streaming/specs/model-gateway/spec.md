# Spec Delta

## ADDED Requirements

### Requirement: 流式对话补全

对话网关 MUST 支持流式补全调用：服务端 MUST 消费网关返回的增量（如 OpenAI 兼容的 `stream: true` 响应），并把新增文本作为 `delta` 事件转发，MUST NOT 等整段回答生成完毕才产生第一个文本片段。网关地址、密钥与模型 MUST 仍只由服务端读取，MUST NOT 下发浏览器；显式配置优先与回退规则 MUST 与既有要求一致。

#### Scenario: 消费上游增量并转发

- **WHEN** 服务端调用对话网关的流式补全接口
- **THEN** 系统 MUST 在收到每个增量时即可向外转发对应文本
- **AND** MUST NOT 等待完整回答后才转发首个文本片段

#### Scenario: 流式调用沿用网关配置与回退

- **WHEN** 仅配置了嵌入网关地址与密钥，未显式配置对话网关
- **THEN** 流式补全 MUST 仍使用嵌入网关的地址与密钥
- **AND** MUST 使用配置的对话模型

#### Scenario: 网关配置不下发浏览器

- **WHEN** 前端发起或接收流式回答
- **THEN** 响应与脚本中 MUST NOT 出现对话网关地址、密钥或模型凭据
