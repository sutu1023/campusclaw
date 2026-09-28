"""Database schema and seed constants.

Kept free of Flask imports so ``scripts/init_db.py`` can create and seed the
database without an application context.
"""

SCHEMA_SQL = """
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS classes (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('teacher', 'student')),
    class_id      INTEGER NOT NULL REFERENCES classes(id),
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 讲义
CREATE TABLE IF NOT EXISTS handouts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    class_id   INTEGER NOT NULL REFERENCES classes(id),
    title      TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 作业
CREATE TABLE IF NOT EXISTS homework (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    class_id   INTEGER NOT NULL REFERENCES classes(id),
    title      TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 助手
CREATE TABLE IF NOT EXISTS assistants (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    class_id   INTEGER NOT NULL REFERENCES classes(id),
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 技能
CREATE TABLE IF NOT EXISTS skills (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS materials (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    class_id    INTEGER NOT NULL REFERENCES classes(id),
    title       TEXT NOT NULL,
    file_path   TEXT,
    uploaded_by INTEGER REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS knowledge_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    material_id INTEGER NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    class_id    INTEGER NOT NULL REFERENCES classes(id),
    body_text   TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_materials_class ON materials(class_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_class ON knowledge_entries(class_id);
"""

# Seeded classes: (id, name)
SEED_CLASSES = [
    (1, "班级 A"),
    (2, "班级 B"),
]

# Fallback plaintext for seeded demo accounts (documented in README.md).
DEFAULT_SEED_PASSWORD = "CampusClaw#2026"

# Seeded users: (username, display_name, role, class_id)
SEED_USERS = [
    ("teacher_a", "教师 A", "teacher", 1),
    ("student_a1", "学生 A1", "student", 1),
    ("student_b1", "学生 B1", "student", 2),
]

# Seeded materials: (class_id, title). Titles are distinguishable per class.
SEED_MATERIALS = [
    (1, "A 班-开学第一课教案.md"),
    (2, "B 班-开学第一课教案.md"),
]

# Placeholder rows for the remaining core structures: (class_id, title)
SEED_HANDOUTS = [(1, "A 班讲义占位")]
SEED_HOMEWORK = [(1, "A 班作业占位")]
SEED_ASSISTANTS = [(1, "A 班助手占位")]
SEED_SKILLS = [("占位技能",)]
