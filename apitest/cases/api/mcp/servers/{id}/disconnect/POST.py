"""POST /api/mcp/servers/{id}/disconnect - revoke without destroying history."""
from client import request
from mcp_helpers import check_turn, server_path

ORDER = 95
METHOD = "POST"
PATH = "/api/mcp/servers/{id}/disconnect"


def run(ctx):
    path = server_path(ctx)
    first = request(ctx["base_url"], METHOD, path + "/disconnect", token=ctx["teacher_token"])
    again = request(ctx["base_url"], METHOD, path + "/disconnect", token=ctx["teacher_token"])
    tools = request(ctx["base_url"], "GET", path + "/tools", token=ctx["teacher_token"])
    results = [("断开与重复断开均返回 204", first.status == 204 and again.status == 204),
               ("断开后回读目录为空", tools.status == 200 and tools.json().get("tools") == [] and tools.json().get("server", {}).get("state") == "disconnected")]
    if ctx.get("mcp_skill_id"):
        saved = request(ctx["base_url"], "PUT", f"/api/skills/{ctx['mcp_skill_id']}", token=ctx["teacher_token"], json_body={"source": ctx["mcp_skill_source"]})
        results.append(("断开后原 MCP 引用拒绝保存且错误可读", saved.status == 400 and "服务器" in saved.json().get("error", "")))
        readback = request(ctx["base_url"], "GET", "/api/skills", token=ctx["teacher_token"])
        found = next((s for s in readback.json().get("skills", []) if s.get("id") == ctx["mcp_skill_id"]), {})
        results.append(("失败更新未改变原技能文件", found.get("source") == ctx["mcp_skill_source"]))
    if ctx.get("conversation_id"):
        results.extend(check_turn(ctx, "勾股定理断开工具后怎样继续检查", "denied"))
    return results
