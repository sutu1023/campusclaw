"""POST /api/login - sign in as a teacher and a student."""

from client import request

ORDER = 10
METHOD = "POST"
PATH = "/api/login"


def run(ctx):
    results = []
    resp = request(
        ctx["base_url"], "POST", "/api/login",
        json_body={"username": "teacher_a", "password": "CampusClaw#2026"},
    )
    results.append(("教师登录返回 200", resp.status == 200))
    body = resp.json()
    ctx["teacher_token"] = body.get("token")
    results.append(("返回教师访问令牌", bool(ctx["teacher_token"])))
    results.append(("教师角色为 teacher", body.get("user", {}).get("role") == "teacher"))

    student = request(
        ctx["base_url"], "POST", "/api/login",
        json_body={"username": "student_a1", "password": "CampusClaw#2026"},
    )
    ctx["student_token"] = student.json().get("token")
    results.append(("学生登录返回访问令牌", bool(ctx["student_token"])))

    wrong = request(
        ctx["base_url"], "POST", "/api/login",
        json_body={"username": "teacher_a", "password": "definitely-wrong"},
    )
    results.append(("错误密码返回 401", wrong.status == 401))
    return results
