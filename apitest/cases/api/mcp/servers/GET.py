"""GET /api/mcp/servers - teacher-only class catalog, without credentials."""
from client import request
from mcp_helpers import public_only, settings

ORDER = 31
METHOD = "GET"
PATH = "/api/mcp/servers"


def run(ctx):
    name, _ = settings(ctx)
    response = request(ctx["base_url"], METHOD, PATH, token=ctx["teacher_token"])
    body = response.json()
    for server in body.get("servers", []):
        if server.get("name") == name:
            ctx["mcp_server_id"] = server["id"]
    denied = request(ctx["base_url"], METHOD, PATH, token=ctx["student_token"])
    anonymous = request(ctx["base_url"], METHOD, PATH)
    return [("教师可列出本班 MCP 登记", response.status == 200 and isinstance(body.get("servers"), list)),
            ("目录无 MCP 密钥或凭据引用", public_only(body)),
            ("学生无 MCP 管理读取权限", denied.status == 403),
            ("未登录 MCP 管理返回 401", anonymous.status == 401)]
