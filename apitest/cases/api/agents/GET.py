"""GET /api/agents - the class agent list."""

from client import request

ORDER = 50
METHOD = "GET"
PATH = "/api/agents"


def run(ctx):
    results = []
    resp = request(ctx["base_url"], "GET", "/api/agents", token=ctx["teacher_token"])
    results.append(("列出本班智能体返回 200", resp.status == 200))
    agents = resp.json().get("agents", [])
    results.append(("返回智能体数组", isinstance(agents, list)))
    return results
