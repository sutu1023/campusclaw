# 验收报告：add-auth-rbac-class-knowledge

对应 OpenSpec change `add-auth-rbac-class-knowledge` 的 tasks 7.2。以下三条为
`specs/auth-upload/spec.md` 的关键 Scenario，均在 Docker Compose 启动的实例上手工复验。

运行环境：`docker compose up --build -d`，应用监听 `http://localhost:8080`。
预置账号密码：`teacher_a` / `student_a1` / `student_b1`，口令 `CampusClaw#2026`。

| # | Spec Scenario | 操作 | 期望 | 实测 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 跨班按 ID 访问材料被拒绝 | `teacher_a` 会话请求 `GET /api/materials/2`（B 班材料） | `404`，响应无 B 班标题/正文/路径 | `HTTP 404`，body `{"error":"not found"}`，无「B 班」内容 | 通过 |
| 2 | 学生上传被拒绝（403） | `student_a1` 会话 `POST /api/materials/upload` 提交 `.md` | `403`，`materials`/`knowledge_entries` 与上传目录无新增 | `HTTP 403`，body `{"error":"forbidden"}`，行数与文件数不变 | 通过 |
| 3 | 上传后知识库与列表可查 | `teacher_a` 上传 `验收-第三场景材料`，随后 `student_a1` 请求 `GET /api/materials` | `201` 且学生本班列表可见该标题 | `HTTP 201`（material_id=3）；`student_a1` 列表含「验收-第三场景材料」 | 通过 |

补充：跨班统一返回 `404`（实现择一，已在 README「权限与班级隔离约定」中固定）。
`openspec validate add-auth-rbac-class-knowledge --strict` 返回 valid（exit 0）。
