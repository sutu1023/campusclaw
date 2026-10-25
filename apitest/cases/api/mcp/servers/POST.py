"""POST /api/mcp/servers - reuse a fixed approved name for repeatable runs."""
from client import request
from mcp_helpers import public_only, server_path, settings

ORDER = 32
METHOD = "POST"
PATH = "/api/mcp/servers"


def run(ctx):
    name, origin = settings(ctx)
    body = {"name": name, "base_url": origin, "class_id": 999999}
    results = []
    if ctx.get("mcp_server_id"):
        response = request(ctx["base_url"], "GET", server_path(ctx), token=ctx["teacher_token"])
        results.append(("复用本班已存在批准登记，不删除固定名称", response.status == 200))
    else:
        response = request(ctx["base_url"], METHOD, PATH, token=ctx["teacher_token"], json_body=body)
        results.append(("登记部署批准的 MCP 服务返回 201", response.status == 201))
        ctx["mcp_server_id"] = response.json().get("id")
    server = response.json()
    results.append(("班级不采信浏览器伪造 class_id", server.get("class_id") != 999999 and bool(server.get("id"))))
    results.append(("服务器只暴露 has_token", isinstance(server.get("has_token"), bool) and public_only(server)))
    duplicate = request(ctx["base_url"], METHOD, PATH, token=ctx["teacher_token"], json_body=body)
    results.append(("班内同名登记返回 400", duplicate.status == 400))
    denied = request(ctx["base_url"], METHOD, PATH, token=ctx["student_token"], json_body=body)
    results.append(("学生登记 MCP 返回 403", denied.status == 403))
    return results
