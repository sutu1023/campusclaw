"""GET /api/mcp/servers/{id}/tools - read complete discovered definitions."""
from client import request
from mcp_helpers import public_only, server_path

ORDER = 36
METHOD = "GET"
PATH = "/api/mcp/servers/{id}/tools"


def run(ctx):
    response = request(ctx["base_url"], METHOD, server_path(ctx) + "/tools", token=ctx["teacher_token"])
    body = response.json()
    tools = body.get("tools", [])
    ctx["mcp_tools"] = tools
    expected = {"formula_hint", "unit_convert"}
    return [("目录含两个教学工具", response.status == 200 and {t.get("name") for t in tools} == expected),
            ("完整说明与输入结构可见", bool(tools) and all(t.get("description") and t.get("inputSchema", {}).get("type") == "object" for t in tools)),
            ("工具目录无凭据字段", public_only(body))]
