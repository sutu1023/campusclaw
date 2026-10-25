"""POST /api/mcp/servers/{id}/connect - real teaching service, never a mock."""
from client import request
from mcp_helpers import public_only, server_path

ORDER = 35
METHOD = "POST"
PATH = "/api/mcp/servers/{id}/connect"


def run(ctx):
    response = request(ctx["base_url"], METHOD, server_path(ctx) + "/connect", token=ctx["teacher_token"])
    body = response.json()
    return [("真实教学 MCP 完成握手和发现", response.status == 200 and body.get("state") == "connected"),
            ("服务端匹配凭据已配置但不回传", body.get("has_token") is True and public_only(body)),
            ("发现快照已批准", bool(body.get("approved_hash")) and not body.get("pending_hash"))]
