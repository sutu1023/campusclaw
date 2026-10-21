"""GET /api/skills - the class skill list (includes seeded skills)."""

from client import request

ORDER = 40
METHOD = "GET"
PATH = "/api/skills"


def run(ctx):
    results = []
    resp = request(ctx["base_url"], "GET", "/api/skills", token=ctx["teacher_token"])
    results.append(("列出本班技能返回 200", resp.status == 200))
    skills = resp.json().get("skills", [])
    results.append(("本班存在种子技能", len(skills) > 0))
    names = [s.get("name") for s in skills]
    results.append(("预置「数学解题引导」存在", "数学解题引导" in names))
    seeded = next((s for s in skills if s.get("name") == "数学解题引导"), None)
    if seeded:
        ctx["seed_skill_id"] = seeded["id"]
    return results
