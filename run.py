"""Local development entry point.

For container/production use the gunicorn factory target ``app:create_app()``
(see ``entrypoint.sh``).
"""

import os

from app import create_app

app = create_app()

if __name__ == "__main__":
    app.run(
        host="0.0.0.0",
        port=int(os.environ.get("PORT", "8080")),
        debug=os.environ.get("FLASK_DEBUG") == "1",
    )
