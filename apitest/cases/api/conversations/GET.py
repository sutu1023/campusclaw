"""GET /api/conversations and /api/conversations/{id} - ownership isolation."""

from client import request

ORDER = 75
METHOD = "GET"
PATH = "/api/conversations"


def run(ctx):
    results = []
    conv_id = ctx["conversation_id"]

    listing = request(ctx["base_url"], "GET", "/api/conversations", token=ctx["student_token"])
    results.append(("列出我的会话返回 200", listing.status == 200))
    ids = [c.get("id") for c in listing.json().get("conversations", [])]
    results.append(("列表中包含刚创建的会话", conv_id in ids))

    detail = request(ctx["base_url"], "GET", f"/api/conversations/{conv_id}", token=ctx["student_token"])
    results.append(("读取本人会话返回 200", detail.status == 200))
    results.append(("新会话暂无消息", detail.json().get("messages") == []))

    cross = request(ctx["base_url"], "GET", f"/api/conversations/{conv_id}", token=ctx["teacher_token"])
    results.append(("读取他人会话返回 404", cross.status == 404))
    return results
