"""POST /api/skills - teacher creates a skill from a file; students are denied."""

import time

from client import request
from mcp_helpers import settings

ORDER = 45
METHOD = "POST"
PATH = "/api/skills"


def run(ctx):
    results = []
    suffix = str(int(time.time() * 1000))
    name = f"覆盖测试技能{suffix}"
    source = (
        f"名称：{name}\n"
        '适用条件：勾股,方程\n'
        '资料：数学\n'
        '工具：knowledge_search\n'
        '启用：是\n'
        '\n'
        '只提示下一步该检查什么，不写出最终得数。\n'
    )

    resp = request(
        ctx["base_url"], "POST", "/api/skills",
        token=ctx["teacher_token"], json_body={"source": source},
    )
    results.append(("教师新建技能返回 201", resp.status == 201))
    created = resp.json()
    ctx["skill_id"] = created.get("id")
    results.append(("解析出的名称与文件一致", created.get("name") == name))
    results.append(("工具白名单解析正确", created.get("tools") == ["knowledge_search"]))
    results.append(("原文保留可回读", created.get("source") == source))

    # Duplicate name inside the class is rejected.
    dup = request(
        ctx["base_url"], "POST", "/api/skills",
        token=ctx["teacher_token"], json_body={"source": source},
    )
    results.append(("班内同名返回 400", dup.status == 400))

    # Invalid file is rejected with a readable message.
    bad = request(
        ctx["base_url"], "POST", "/api/skills",
        token=ctx["teacher_token"], json_body={"source": "适用条件：勾股\n启用：是\n\n正文"},
    )
    results.append(("缺少名称返回 400", bad.status == 400))
    results.append(("错误信息可读（含「名称」）", "名称" in bad.json().get("error", "")))

    # Students cannot write.
    denied = request(
        ctx["base_url"], "POST", "/api/skills",
        token=ctx["student_token"], json_body={"source": source},
    )
    results.append(("学生写技能返回 403", denied.status == 403))

    server_name, _ = settings(ctx)
    refs = ["knowledge_search", f"formula_hint@{server_name}", f"unit_convert@{server_name}"]
    mcp_source = source.replace(name, f"MCP测试技能{suffix}").replace(
        "工具：knowledge_search", "工具：" + ",".join(refs),
    )
    bound = request(ctx["base_url"], "POST", "/api/skills", token=ctx["teacher_token"], json_body={"source": mcp_source})
    mcp_skill = bound.json()
    results.append(("发现后 MCP 工具引用可保存", bound.status == 201 and mcp_skill.get("tools") == refs))
    results.append(("MCP 技能完整原文保留", mcp_skill.get("source") == mcp_source))
    ctx["mcp_skill_id"] = mcp_skill.get("id")
    ctx["mcp_skill_source"] = mcp_source
    ctx["mcp_refs"] = refs[1:]
    if ctx["mcp_skill_id"]:
        ctx["builtin_skill_id"] = ctx["skill_id"]
        ctx["skill_id"] = ctx["mcp_skill_id"]
        readback = request(ctx["base_url"], "GET", "/api/skills", token=ctx["teacher_token"])
        found = next((s for s in readback.json().get("skills", []) if s.get("id") == ctx["mcp_skill_id"]), {})
        results.append(("回读 MCP 技能原文与全部引用一致", found.get("source") == mcp_source and found.get("tools") == refs))
    return results
