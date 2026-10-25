"""POST messages: real SSE, safe summaries and database-backed replay.

The 参数校验演练 question is an explicit fault scenario in the deterministic
verification gateway. A real gateway must produce an actual error outcome too;
no empty audit or unavailable gateway is accepted as success.
"""
from client import request
from mcp_helpers import check_turn

ORDER = 80
METHOD = "POST"
PATH = "/api/conversations/{id}/messages"


def run(ctx):
    path = f"/api/conversations/{ctx['conversation_id']}/messages"
    short = request(ctx["base_url"], "POST", path, token=ctx["student_token"], json_body={"content": "短"})
    results = [("过短提问返回 400", short.status == 400)]
    results.extend(check_turn(ctx, "勾股定理怎么用并统一长度单位", "ok"))
    results.extend(check_turn(ctx, "勾股参数校验演练，请在工具错误后继续引导", "error"))
    return results
