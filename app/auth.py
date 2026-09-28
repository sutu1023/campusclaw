"""Login, logout and server-side authorization decorators."""

import functools

from flask import (
    Blueprint,
    abort,
    jsonify,
    redirect,
    render_template,
    request,
    session,
    url_for,
)

from . import db
from .passwords import verify_password

bp = Blueprint("auth", __name__)


def _wants_json() -> bool:
    return request.path.startswith("/api/") or request.is_json


def _unauthorized():
    if _wants_json():
        return jsonify(error="unauthorized"), 401
    return redirect(url_for("auth.login", next=request.path))


def login_required(view):
    @functools.wraps(view)
    def wrapped(*args, **kwargs):
        if not session.get("user_id"):
            return _unauthorized()
        return view(*args, **kwargs)

    return wrapped


def teacher_required(view):
    @functools.wraps(view)
    def wrapped(*args, **kwargs):
        if not session.get("user_id"):
            return _unauthorized()
        if session.get("role") != "teacher":
            if _wants_json():
                return jsonify(error="forbidden"), 403
            abort(403)
        return view(*args, **kwargs)

    return wrapped


@bp.route("/login", methods=["GET", "POST"])
def login():
    if request.method == "POST":
        username = (request.form.get("username") or "").strip()
        password = request.form.get("password") or ""
        user = db.get_user_by_username(username)
        if user is None or not verify_password(password, user["password_hash"]):
            return render_template("login.html", error="用户名或密码错误"), 401

        session.clear()
        session["user_id"] = user["id"]
        session["username"] = user["username"]
        session["role"] = user["role"]
        session["class_id"] = user["class_id"]
        return redirect(url_for("materials.index"))

    return render_template("login.html")


@bp.route("/logout", methods=["GET", "POST"])
def logout():
    session.clear()
    return redirect(url_for("auth.login"))
