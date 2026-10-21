"""POST /api/conversations/{id}/messages - SSE streaming answer and persistence."""

from client import read_sse, request

ORDER = 80
METHOD = "POST"
PATH = "/api/conversations/{id}/messages"


def run(ctx):
    results = []
    conv_id = ctx["conversation_id"]
    path = f"/api/conversations/{conv_id}/messages"

    too_short = request(
        ctx["base_url"], "POST", path,
        token=ctx["student_token"], json_body={"content": "短"},
    )
    results.append(("过短提问返回 400", too_short.status == 400))

    resp = request(
        ctx["base_url"], "POST", path,
        token=ctx["student_token"], json_body={"content": "勾股定理怎么用"},
        timeout=120,
    )
    if resp.status == 200:
        events = read_sse(resp)
        results.append(("meta 最先到达", bool(events) and events[0][0] == "meta"))
        results.append(("至少推送一个 delta", any(e[0] == "delta" for e in events)))
        results.append(("以 done 收尾", bool(events) and events[-1][0] == "done"))
        concat = "".join(e[1].get("text", "") for e in events if e[0] == "delta")

        # Read back through the API (which reads MySQL) and compare.
        detail = request(ctx["base_url"], "GET", f"/api/conversations/{conv_id}", token=ctx["student_token"])
        messages = detail.json().get("messages", [])
        results.append(("助手消息已落库", len(messages) >= 2 and messages[-1].get("role") == "assistant"))
        if messages:
            results.append(("delta 拼接等于落库正文", concat == messages[-1].get("content")))
    else:
        # Without a configured chat gateway the handler reports 503 before the
        # stream starts; that is acceptable for a stack without a gateway.
        results.append(("未配置对话网关时返回 503（跳过流式断言）", resp.status == 503))
    return results
