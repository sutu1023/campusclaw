"""Create the CampusClaw database schema and seed demo data.

Safe to run repeatedly: tables use ``IF NOT EXISTS`` and seed rows are only
inserted when missing. Called by the container entrypoint when no database
file exists yet, or manually via ``python scripts/init_db.py``.
"""

import os
import sqlite3
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from app import schema  # noqa: E402
from app.passwords import hash_password  # noqa: E402

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def default_db_path() -> str:
    return os.environ.get("DATABASE_PATH", os.path.join(BASE_DIR, "data", "app.db"))


def _seed(conn: sqlite3.Connection, password: str) -> None:
    conn.executemany(
        "INSERT OR IGNORE INTO classes (id, name) VALUES (?, ?)",
        schema.SEED_CLASSES,
    )

    for username, display_name, role, class_id in schema.SEED_USERS:
        exists = conn.execute(
            "SELECT 1 FROM users WHERE username = ?", (username,)
        ).fetchone()
        if not exists:
            conn.execute(
                "INSERT INTO users "
                "(username, display_name, password_hash, role, class_id) "
                "VALUES (?, ?, ?, ?, ?)",
                (
                    username,
                    display_name,
                    hash_password(password),
                    role,
                    class_id,
                ),
            )

    for class_id, title in schema.SEED_MATERIALS:
        exists = conn.execute(
            "SELECT 1 FROM materials WHERE class_id = ? AND title = ?",
            (class_id, title),
        ).fetchone()
        if not exists:
            conn.execute(
                "INSERT INTO materials (class_id, title, file_path) "
                "VALUES (?, ?, NULL)",
                (class_id, title),
            )

    placeholder_specs = [
        ("handouts", "title", schema.SEED_HANDOUTS),
        ("homework", "title", schema.SEED_HOMEWORK),
        ("assistants", "name", schema.SEED_ASSISTANTS),
    ]
    for table, column, rows in placeholder_specs:
        count = conn.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
        if count == 0:
            conn.executemany(
                f"INSERT INTO {table} (class_id, {column}) VALUES (?, ?)",
                rows,
            )

    skill_count = conn.execute("SELECT COUNT(*) FROM skills").fetchone()[0]
    if skill_count == 0:
        conn.executemany(
            "INSERT INTO skills (name) VALUES (?)",
            schema.SEED_SKILLS,
        )


def init_db(db_path: str | None = None, password: str | None = None) -> str:
    db_path = db_path or default_db_path()
    directory = os.path.dirname(db_path)
    if directory:
        os.makedirs(directory, exist_ok=True)

    conn = sqlite3.connect(db_path)
    conn.execute("PRAGMA foreign_keys = ON")
    try:
        conn.executescript(schema.SCHEMA_SQL)
        _seed(conn, password or schema.DEFAULT_SEED_PASSWORD)
        conn.commit()
    finally:
        conn.close()
    return db_path


if __name__ == "__main__":
    path = init_db()
    print(f"Initialized database at {path}")
