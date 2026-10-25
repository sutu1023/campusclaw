"""Minimal HTTP client for the CampusClaw API tests (standard library only)."""

import json
import urllib.error
import urllib.request


class Response:
    def __init__(self, status, headers, body):
        self.status = status
        self.headers = headers
        self.body = body

    @property
    def text(self):
        return self.body.decode("utf-8", "replace")

    def json(self):
        try:
            return json.loads(self.text or "{}")
        except json.JSONDecodeError:
            return {}


def request(base_url, method, path, token=None, json_body=None, timeout=60):
    """Send one request and always return a Response (HTTP errors included)."""
    url = base_url.rstrip("/") + path
    data = None
    headers = {}
    if json_body is not None:
        data = json.dumps(json_body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return Response(resp.status, dict(resp.headers), resp.read())
    except urllib.error.HTTPError as exc:
        with exc:
            return Response(exc.code, dict(exc.headers or {}), exc.read())


def read_sse(response):
    """Parse an SSE body into a list of (event, data) tuples."""
    events = []
    for block in response.text.split("\n\n"):
        block = block.strip()
        if not block:
            continue
        event = ""
        data = ""
        for line in block.split("\n"):
            if line.startswith("event:"):
                event = line[len("event:"):].strip()
            elif line.startswith("data:"):
                data += line[len("data:"):].strip()
        if not event:
            continue
        try:
            parsed = json.loads(data)
        except json.JSONDecodeError:
            parsed = data
        events.append((event, parsed))
    return events
