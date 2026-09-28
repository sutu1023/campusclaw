"""CampusClaw application factory.

The session signing key (``SECRET_KEY``) is read only from the environment.
When it is missing the application refuses to start, so a deployment can never
silently fall back to an insecure default (see ``design.md``).
"""

import os

from flask import Flask

from . import db
from .auth import bp as auth_bp
from .materials import bp as materials_bp

# Repository root (parent of the ``app`` package).
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def create_app(test_config=None):
    app = Flask(__name__)

    secret_key = os.environ.get("SECRET_KEY")
    if test_config and test_config.get("SECRET_KEY"):
        secret_key = test_config["SECRET_KEY"]
    if not secret_key:
        raise RuntimeError(
            "SECRET_KEY environment variable is required and must not be "
            "hardcoded. Copy .env.example to .env and set SECRET_KEY."
        )

    app.config.from_mapping(
        SECRET_KEY=secret_key,
        DATABASE_PATH=os.environ.get(
            "DATABASE_PATH", os.path.join(BASE_DIR, "data", "app.db")
        ),
        UPLOAD_DIR=os.environ.get("UPLOAD_DIR", os.path.join(BASE_DIR, "uploads")),
        SESSION_COOKIE_HTTPONLY=True,
        SESSION_COOKIE_SAMESITE="Lax",
        MAX_CONTENT_LENGTH=16 * 1024 * 1024,
    )
    if test_config:
        app.config.update(test_config)

    app.register_blueprint(auth_bp)
    app.register_blueprint(materials_bp)

    @app.route("/health", methods=["GET"])
    def health():
        # Public liveness endpoint: no login required.
        return {"status": "ok"}

    app.teardown_appcontext(db.close_conn)

    return app
