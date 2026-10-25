"""GET /api/mcp/audit - teacher-only paginated, filtered safe evidence."""
from client import request
from mcp_helpers import public_only

ORDER = 110
METHOD = "GET"
PATH = "/api/mcp/audit"


def run(ctx):
    response = request(ctx["base_url"], METHOD, PATH + "?page=1&page_size=20", token=ctx["teacher_token"])
    body = response.json()
    results = [("审计分页返回完整结构", response.status == 200 and isinstance(body.get("calls"), list) and body.get("page_size") == 20),
               ("审计只返回安全摘要", public_only(body))]
    ids = [row.get("id", 0) for row in body.get("calls", [])]
    results.append(("审计按新到旧排序", ids == sorted(ids, reverse=True)))
    for status in ("ok", "error", "denied"):
        filtered = request(ctx["base_url"], METHOD, PATH + f"?status={status}&server_id={ctx['mcp_server_id']}", token=ctx["teacher_token"])
        calls = filtered.json().get("calls", [])
        results.append((f"审计支持非空 {status} 与服务器筛选", filtered.status == 200 and bool(calls) and all(c.get("status") == status for c in calls)))
        turns = [turn for turn in ctx.get("mcp_turns", []) if turn["status"] == status]
        expected = [(turn, call) for turn in turns for call in turn["calls"]]
        matches = []
        for turn, summary in expected:
            row = next((c for c in calls if c.get("call_id") == summary.get("call_id")), {})
            matches.append(bool(row) and row.get("user_id") == ctx.get("student_id")
                           and row.get("conversation_id") == ctx.get("conversation_id")
                           and row.get("question_message_id") == turn["question_id"]
                           and all(row.get(key) == summary.get(key) for key in ("ref", "round", "status", "text")))
        results.append((f"本轮 {status} 审计匹配用户/会话/提问/轮次/摘要", bool(expected) and all(matches)))
    denied = request(ctx["base_url"], METHOD, PATH, token=ctx["student_token"])
    results.append(("学生审计读取返回 403", denied.status == 403))
    return results
