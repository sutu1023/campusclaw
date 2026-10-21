"""GET /api/me - current user and unauthenticated rejection."""

from client import request

ORDER = 20
METHOD = "GET"
PATH = "/api/me"


def run(ctx):
    results = []
    me = request(ctx["base_url"], "GET", "/api/me", token=ctx["teacher_token"])
    results.append(("带令牌读取当前用户返回 200", me.status == 200))
    results.append(("返回教师角色", me.json().get("user", {}).get("role") == "teacher"))

    anon = request(ctx["base_url"], "GET", "/api/me")
    results.append(("未登录返回 401", anon.status == 401))
    return results
