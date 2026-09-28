# CampusClaw

CampusClaw 是一个面向中小学教学与教研场景的智能平台，旨在利用大语言模型、智能体和知识库等技术，辅助教师完成备课、命题、学情分析、教研问答等工作，提升教学与教研效率。

## 项目价值

CampusClaw 面向中小学教师的实际教学与教研场景，解决教学资源分散、重复性工作较多、教研知识难以沉淀与共享等问题。

通过 AI、知识库、智能体等技术，将教学资源、班级学情和教研知识连接起来，为教师提供可使用、可追溯、可持续积累的智能教学与教研能力。

## 本学期目标

本课程以 CampusClaw 为案例，按照规约驱动开发的方式，逐步完成一个可部署、可演示的智能教学平台。

课程将逐步实现：

* 用户认证、角色权限与班级隔离
* 教学材料上传与知识库入库
* 可追溯的知识库检索
* AI 技能与流式会话
* MCP 工具接入
* 作业布置、提交与批改
* 异步任务与消息队列
* 安全检查与审计
* 自动化测试、部署与运行验证

## 本学期暂不做

当前阶段不提前实现后续课程内容。就本变更（认证、角色权限与班级隔离）而言，明确不做：

- 检索与问答（RAG、全文检索、智能问答）
- 对话助手（聊天界面、Agent 编排、技能包运行时）
- 作业流程（提交、批改、成绩、审计、错题本）
- 身份与企业集成（多校/多租户、OAuth/OIDC、SSO、短信/邮箱验证码）
- 生产运维（K8s、多副本高可用、完整 CI/CD）
- 以前端隐藏按钮替代服务端权限与班级隔离

具体功能按照课程进度逐步开发，未进入对应课程阶段的功能暂不实现。

## 快速开始（Docker Compose）

前置条件：已安装 Docker 与 Docker Compose v2。

```bash
# 1. 准备环境变量（SECRET_KEY 必填，缺失时应用拒绝启动）
cp .env.example .env
# 生成一个密钥，并把 .env 中的 SECRET_KEY= 替换为该值：
python -c "import secrets; print(secrets.token_hex(32))"

# 2. 构建并启动（首次启动会自动建表并写入种子数据）
docker compose up --build -d
```

启动后可访问：

- 登录页：<http://localhost:8080/login>
- 健康检查：<http://localhost:8080/health>（无需登录，返回 JSON `{"status":"ok"}`）
- 本班材料列表：<http://localhost:8080/materials>（需登录）

### 预置账号（演示用）

| 用户名 | 角色 | 班级 | 密码 |
| --- | --- | --- | --- |
| `teacher_a` | teacher | 班级 A | `CampusClaw#2026` |
| `student_a1` | student | 班级 A | `CampusClaw#2026` |
| `student_b1` | student | 班级 B | `CampusClaw#2026` |

密码以 bcrypt 哈希存入 `users.password_hash`，库中不存在明文。

### `.env` 变量说明

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `SECRET_KEY` | 是 | 无 | 会话签名密钥，仅从环境变量读取，源码中不硬编码 |
| `DATABASE_PATH` | 否 | `/app/data/app.db`（Compose） | SQLite 数据库文件路径 |
| `UPLOAD_DIR` | 否 | `/app/uploads`（Compose） | 上传文件落盘目录 |
| `GUNICORN_WORKERS` | 否 | `2` | gunicorn 工作进程数 |

### 数据持久化

Compose 将宿主机的 `./data` 与 `./uploads` 挂载到容器内。`docker compose down` 后再 `docker compose up`（不删除这两个目录）时，预置账号、已上传材料与知识库记录仍然存在。删除 `data/app.db` 后再次 `up`，entrypoint 会自动重新初始化。

### 本地开发（不使用 Docker）

```bash
pip install -r requirements.txt
SECRET_KEY=dev-secret python scripts/init_db.py
SECRET_KEY=dev-secret python run.py   # http://localhost:8080
```

## 权限与班级隔离约定

- 未登录访问受保护页面重定向到 `/login`；访问受保护 API 返回 `401`。
- 学生调用上传接口返回 `403`，且不产生数据库行或落盘文件。
- 跨班按 ID 访问材料统一返回 **`404`**（不区分「不存在」与「无权限」，避免泄露他班资源是否存在），响应体不含他班标题、正文或路径。
- 所有业务查询按会话中的 `class_id` 过滤，不接受客户端传入的 `class_id` 覆盖。

## 开发方式

本项目采用 SDD（Specification-Driven Development，规约驱动开发）。

开发遵循：

> 无规约，不写代码。

需求先形成可执行、可验收的规约，再根据规约进行开发和验收。
