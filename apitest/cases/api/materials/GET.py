"""GET /api/materials - the class material list."""

from client import request

ORDER = 30
METHOD = "GET"
PATH = "/api/materials"


def run(ctx):
    results = []
    resp = request(
        ctx["base_url"], "GET", "/api/materials?page=1&page_size=100",
        token=ctx["teacher_token"],
    )
    results.append(("列出本班材料返回 200", resp.status == 200))
    materials = resp.json().get("materials", [])
    results.append(("本班存在材料（种子数据）", len(materials) > 0))
    results.append(("列表仅含本班（class_id=1）", all(m.get("class_id") == 1 for m in materials)))
    if materials:
        ctx["material_id"] = materials[0]["id"]
        ctx["material_title"] = materials[0]["title"]
    return results
