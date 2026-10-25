"""Non-secret MCP test helpers. The suite never reads or submits MCP tokens."""

import os

from client import read_sse, request


def settings(ctx):
    ctx.setdefault("mcp_server_name", os.environ.get("APITEST_MCP_SERVER_NAME", "campus-tools"))
    ctx.setdefault("mcp_base_url", os.environ.get("APITEST_MCP_BASE_URL", "http://mcp-demo:8090"))
    return ctx["mcp_server_name"], ctx["mcp_base_url"]


def server_path(ctx):
    if not ctx.get("mcp_server_id"):
        raise RuntimeError("approved MCP registration was not created or located")
    return f'/api/mcp/servers/{ctx["mcp_server_id"]}'


def public_only(value):
    """has_token is public; recoverable credentials and references are not."""
    private = {"token", "auth_token", "credential_ref", "token_env", "api_key", "authorization"}
    if isinstance(value, dict):
        return not (private & {str(k).lower() for k in value}) and all(public_only(v) for v in value.values())
    if isinstance(value, list):
        return all(public_only(v) for v in value)
    return True


def check_turn(ctx, question, expected):
    """Verify SSE, DB-backed replay and exact audit correlation for one turn."""
    label = expected + " 提问"
    conv_id = ctx["conversation_id"]
    response = request(ctx["base_url"], "POST", f"/api/conversations/{conv_id}/messages",
                       token=ctx["student_token"], json_body={"content": question}, timeout=120)
    results = [(label + "返回真实 SSE（503 不算通过）", response.status == 200 and "text/event-stream" in response.headers.get("Content-Type", ""))]
    if response.status != 200:
        return results
    events = read_sse(response)
    metas = [(i, value) for i, (name, value) in enumerate(events) if name == "meta"]
    deltas = [(i, value) for i, (name, value) in enumerate(events) if name == "delta"]
    calls = [call for _, meta in metas for call in meta.get("tool_calls", [])]
    results.append((label + "meta 先行且 done 收尾", bool(events) and events[0][0] == "meta" and events[-1][0] == "done"))
    results.append((label + "有非空增量正文", bool(deltas) and all(value.get("text") for _, value in deltas)))
    results.append((label + "工具摘要非空且状态准确", bool(calls) and all(call.get("status") == expected for call in calls)))
    ids = [call.get("call_id") for call in calls]
    results.append((label + "call_id 唯一且字段脱敏", bool(ids) and all(ids) and len(ids) == len(set(ids)) and public_only(calls)))
    results.append((label + "全部工具 meta 早于首段 delta", bool(deltas) and bool(calls) and all(i < deltas[0][0] for i, meta in metas if meta.get("tool_calls"))))
    results.append((label + "skill/citations 与外层 ok 稳定", bool(metas) and all(meta.get("status") == "ok" and meta.get("skill") == metas[0][1].get("skill") and meta.get("citations") == metas[0][1].get("citations") for _, meta in metas)))
    if expected == "denied":
        results.append((label + "断开引用均为第零轮拒绝", bool(calls) and all(call.get("round") == 0 for call in calls)))
    detail = request(ctx["base_url"], "GET", f"/api/conversations/{conv_id}", token=ctx["student_token"])
    messages = detail.json().get("messages", [])
    done_id = next((value.get("message_id") for name, value in reversed(events) if name == "done"), None)
    assistant = next((m for m in messages if m.get("id") == done_id and m.get("role") == "assistant"), {})
    user = messages[-2] if len(messages) >= 2 else {}
    text = "".join(value.get("text", "") for _, value in deltas)
    results.append((label + "done 指向落库正文且增量拼接一致", detail.status == 200 and bool(assistant) and bool(text) and assistant.get("content") == text))
    results.append((label + "回放保留完整有序工具摘要", bool(calls) and assistant.get("tool_calls") == calls))
    results.append((label + "本轮仅持久化 user/assistant 对", bool(assistant) and user.get("role") == "user" and user.get("content") == question and messages[-1].get("id") == done_id))
    ctx.setdefault("mcp_turns", []).append({"status": expected, "question_id": user.get("id"), "calls": calls})
    return results
