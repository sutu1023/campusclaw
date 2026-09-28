"""SQLite access helpers.

Every query that touches class-scoped business data takes ``class_id`` as an
explicit argument and filters on it, so callers can only ever read the class
carried by the server-side session.
"""

import sqlite3

from flask import current_app, g


def get_conn():
    if "db" not in g:
        conn = sqlite3.connect(current_app.config["DATABASE_PATH"])
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA foreign_keys = ON")
        g.db = conn
    return g.db


def close_conn(exc=None):
    conn = g.pop("db", None)
    if conn is not None:
        conn.close()


def get_user_by_username(username):
    return get_conn().execute(
        "SELECT id, username, display_name, password_hash, role, class_id "
        "FROM users WHERE username = ?",
        (username,),
    ).fetchone()


def list_materials(class_id):
    """Return only the materials that belong to ``class_id``."""
    return get_conn().execute(
        "SELECT id, class_id, title, file_path, created_at "
        "FROM materials WHERE class_id = ? ORDER BY id DESC",
        (class_id,),
    ).fetchall()


def get_material(material_id):
    return get_conn().execute(
        "SELECT id, class_id, title, file_path, created_at "
        "FROM materials WHERE id = ?",
        (material_id,),
    ).fetchone()


def create_material_with_entry(title, class_id, file_path, uploaded_by, body_text):
    """Insert a material and its knowledge entry in a single transaction."""
    conn = get_conn()
    with conn:  # commits on success, rolls back on exception
        cur = conn.execute(
            "INSERT INTO materials (class_id, title, file_path, uploaded_by) "
            "VALUES (?, ?, ?, ?)",
            (class_id, title, file_path, uploaded_by),
        )
        material_id = cur.lastrowid
        conn.execute(
            "INSERT INTO knowledge_entries (material_id, class_id, body_text) "
            "VALUES (?, ?, ?)",
            (material_id, class_id, body_text),
        )
    return material_id


def list_knowledge_entries(class_id):
    return get_conn().execute(
        "SELECT id, material_id, class_id, created_at "
        "FROM knowledge_entries WHERE class_id = ? ORDER BY id DESC",
        (class_id,),
    ).fetchall()
