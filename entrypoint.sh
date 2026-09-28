#!/bin/sh
# Container entrypoint: initialise the database on first run, then serve.
set -e

DB_PATH="${DATABASE_PATH:-/app/data/app.db}"

if [ ! -f "$DB_PATH" ]; then
    echo "No database at $DB_PATH; initialising schema and seed data..."
    python scripts/init_db.py
fi

exec gunicorn --bind 0.0.0.0:8080 --workers "${GUNICORN_WORKERS:-2}" "app:create_app()"
