"""POST /api/mcp/servers/{id}/approve - never auto-approve unknown changes."""
from client import request
from mcp_helpers import server_path

ORDER = 37
METHOD = "POST"
PATH = "/api/mcp/servers/{id}/approve"


def run(ctx):
    response = request(ctx["base_url"], METHOD, server_path(ctx) + "/approve", token=ctx["teacher_token"], json_body={"snapshot_hash": "0" * 64})
    denied = request(ctx["base_url"], METHOD, server_path(ctx) + "/approve", token=ctx["student_token"], json_body={"snapshot_hash": "0" * 64})
    return [("不匹配或过期快照确认返回 409", response.status == 409),
            ("学生不能确认工具变更", denied.status == 403)]
