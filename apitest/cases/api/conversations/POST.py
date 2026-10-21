"""POST /api/conversations - create a conversation bound to an enabled agent."""

from client import request

ORDER = 70
METHOD = "POST"
PATH = "/api/conversations"


def run(ctx):
    results = []
    resp = request(
        ctx["base_url"], "POST", "/api/conversations",
        token=ctx["student_token"], json_body={"agent_id": ctx["agent_id"]},
    )
    results.append(("按智能体新建会话返回 201", resp.status == 201))
    conv = resp.json()
    ctx["conversation_id"] = conv.get("id")
    results.append(("会话标题为占位串", conv.get("title") == "新的提问"))
    results.append(("会话锁定所选智能体", conv.get("agent_id") == ctx["agent_id"]))

    missing = request(
        ctx["base_url"], "POST", "/api/conversations",
        token=ctx["student_token"], json_body={"agent_id": 999999},
    )
    results.append(("不存在的智能体返回 404", missing.status == 404))
    return results
