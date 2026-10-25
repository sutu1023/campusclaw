"""DELETE route checks. Keep the fixed approved name for repeatable runs.

Successful tombstoning/name retention is exercised by MySQL HTTP integration
cases. Deleting the deployment's fixed name here would permanently prevent the
next run from registering it and destroy a teacher-managed configuration.
"""
from client import request
from mcp_helpers import server_path

ORDER = 97
METHOD = "DELETE"
PATH = "/api/mcp/servers/{id}"


def run(ctx):
    denied = request(ctx["base_url"], METHOD, server_path(ctx), token=ctx["student_token"])
    missing = request(ctx["base_url"], METHOD, "/api/mcp/servers/999999999", token=ctx["teacher_token"])
    return [("学生不能删除 MCP 登记", denied.status == 403),
            ("不存在的 MCP 删除返回 404", missing.status == 404)]
