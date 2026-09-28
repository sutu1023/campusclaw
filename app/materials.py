"""Material listing, detail and teacher upload routes."""

import os
import uuid

from flask import (
    Blueprint,
    abort,
    current_app,
    jsonify,
    redirect,
    render_template,
    request,
    session,
    url_for,
)
from werkzeug.utils import secure_filename

from . import db
from .auth import login_required, teacher_required
from .knowledge import ParseError, parse_text

bp = Blueprint("materials", __name__)

ALLOWED_EXTENSIONS = {".txt", ".md"}


@bp.route("/", methods=["GET"])
def root():
    if session.get("user_id"):
        return redirect(url_for("materials.index"))
    return redirect(url_for("auth.login"))


def _class_id() -> int:
    # The class boundary always comes from the server-side session, never
    # from client-supplied query/body parameters.
    return session["class_id"]


@bp.route("/materials", methods=["GET"])
@login_required
def index():
    materials = db.list_materials(_class_id())
    return render_template(
        "materials.html", materials=materials, role=session.get("role")
    )


@bp.route("/materials/<int:material_id>", methods=["GET"])
@login_required
def detail(material_id):
    material = db.get_material(material_id)
    if material is None or material["class_id"] != _class_id():
        # Same response for missing and cross-class rows: never leak existence.
        abort(404)
    return render_template("material_detail.html", material=material)


@bp.route("/api/materials", methods=["GET"])
@login_required
def api_list():
    materials = db.list_materials(_class_id())
    return jsonify(materials=[dict(row) for row in materials])


@bp.route("/api/materials/<int:material_id>", methods=["GET"])
@login_required
def api_detail(material_id):
    material = db.get_material(material_id)
    if material is None or material["class_id"] != _class_id():
        return jsonify(error="not found"), 404
    return jsonify(material=dict(material))


@bp.route("/api/materials/upload", methods=["POST"])
@teacher_required
def api_upload():
    uploaded = request.files.get("file")
    if uploaded is None or not uploaded.filename:
        return jsonify(error="file is required"), 400

    extension = os.path.splitext(uploaded.filename)[1].lower()
    if extension not in ALLOWED_EXTENSIONS:
        return jsonify(error="unsupported file type"), 400

    raw = uploaded.read()
    try:
        body_text = parse_text(raw)
    except ParseError as exc:
        return jsonify(error=str(exc)), 400

    title = (request.form.get("title") or "").strip() or os.path.splitext(
        os.path.basename(uploaded.filename)
    )[0]

    class_id = _class_id()
    safe_name = secure_filename(uploaded.filename) or f"upload{extension}"
    dest_dir = os.path.join(current_app.config["UPLOAD_DIR"], str(class_id))
    os.makedirs(dest_dir, exist_ok=True)
    stored_name = f"{uuid.uuid4().hex}_{safe_name}"
    dest_path = os.path.join(dest_dir, stored_name)
    with open(dest_path, "wb") as handle:
        handle.write(raw)

    try:
        material_id = db.create_material_with_entry(
            title=title,
            class_id=class_id,
            file_path=dest_path,
            uploaded_by=session["user_id"],
            body_text=body_text,
        )
    except Exception:
        # Keep the filesystem and database consistent on failure.
        if os.path.exists(dest_path):
            os.remove(dest_path)
        raise

    return jsonify(material_id=material_id, title=title), 201
