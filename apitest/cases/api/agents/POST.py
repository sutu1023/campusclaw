"""POST /api/agents - teacher creates an agent bound to a skill and a material."""

import time

from client import request

ORDER = 55
METHOD = "POST"
PATH = "/api/agents"


def run(ctx):
    results = []
    suffix = str(int(time.time() * 1000))
    name = f"接口测试助手{suffix}"
    skill_id = ctx.get("skill_id") or ctx.get("seed_skill_id")
    material_id = ctx.get("material_id")
    body = {
        "name": name,
        "prompt": "用本班数学资料引导学生。",
        "enabled": True,
        "skill_ids": [skill_id],
        "material_ids": [material_id],
    }

    resp = request(ctx["base_url"], "POST", "/api/agents", token=ctx["teacher_token"], json_body=body)
    results.append(("教师新建智能体返回 201", resp.status == 201))
    created = resp.json()
    ctx["agent_id"] = created.get("id")
    results.append(("名称与提交一致", created.get("name") == name))
    results.append(("技能绑定回读一致", created.get("skill_ids") == [skill_id]))
    results.append(("材料绑定回读一致", created.get("material_ids") == [material_id]))

    # DB-backed read-back through the API.
    get = request(ctx["base_url"], "GET", f"/api/agents/{ctx['agent_id']}", token=ctx["teacher_token"])
    results.append(("按编号读回智能体返回 200", get.status == 200))
    results.append(("读回的智能体与写入一致", get.json().get("name") == name))

    denied = request(
        ctx["base_url"], "POST", "/api/agents",
        token=ctx["student_token"], json_body={"name": f"学生助手{suffix}", "enabled": True},
    )
    results.append(("学生新建智能体返回 403", denied.status == 403))

    missing = request(ctx["base_url"], "GET", "/api/agents/999999", token=ctx["teacher_token"])
    results.append(("不存在的智能体返回 404", missing.status == 404))
    return results
