"""POST /api/search - class-scoped hybrid retrieval."""

from client import request

ORDER = 60
METHOD = "POST"
PATH = "/api/search"


def run(ctx):
    results = []
    resp = request(
        ctx["base_url"], "POST", "/api/search",
        token=ctx["student_token"],
        json_body={"q": "勾股", "mode": "hybrid", "limit": 10},
    )
    results.append(("混合检索返回 200", resp.status == 200))
    hits = resp.json().get("hits", [])
    results.append(("命中本班切片", len(hits) > 0))
    if hits:
        results.append(("命中含可回溯字段", "material_title" in hits[0] and "chunk_index" in hits[0]))

    empty = request(
        ctx["base_url"], "POST", "/api/search",
        token=ctx["student_token"], json_body={"q": "", "mode": "hybrid"},
    )
    results.append(("空查询返回 400", empty.status == 400))
    return results
