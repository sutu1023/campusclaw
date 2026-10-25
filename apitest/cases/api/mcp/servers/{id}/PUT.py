"""PUT /api/mcp/servers/{id} - only use the deployment-approved origin."""
from client import request
from mcp_helpers import public_only, server_path, settings

ORDER = 34
METHOD = "PUT"
PATH = "/api/mcp/servers/{id}"


def run(ctx):
    name, origin = settings(ctx)
    response = request(ctx["base_url"], METHOD, server_path(ctx), token=ctx["teacher_token"], json_body={"base_url": origin})
    body = response.json()
    renamed = request(ctx["base_url"], METHOD, server_path(ctx), token=ctx["teacher_token"], json_body={"name": name + "-renamed", "base_url": origin})
    return [("批准地址可保存，名称保持不变", response.status == 200 and body.get("name") == name),
            ("地址更新响应无密钥", public_only(body)),
            ("服务器名称不可修改", renamed.status == 400)]
