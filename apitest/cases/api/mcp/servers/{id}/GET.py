"""GET /api/mcp/servers/{id} - read back the approved registration."""
from client import request
from mcp_helpers import public_only, server_path

ORDER = 33
METHOD = "GET"
PATH = "/api/mcp/servers/{id}"


def run(ctx):
    response = request(ctx["base_url"], METHOD, server_path(ctx), token=ctx["teacher_token"])
    body = response.json()
    missing = request(ctx["base_url"], METHOD, "/api/mcp/servers/999999999", token=ctx["teacher_token"])
    return [("MCP 登记可按编号回读", response.status == 200 and body.get("id") == ctx["mcp_server_id"]),
            ("回读登记无密钥", public_only(body)),
            ("不存在 MCP 编号返回 404", missing.status == 404)]
